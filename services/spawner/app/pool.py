"""Owns every sandbox container: the one always-open floor container, whatever gets
spawned on demand on top of it, and the retention timer that reclaims idle ones.

This is the implementation of the final design decision: 1 container always open
(the floor, supervised and respawned immediately if it dies, exempt from the
retention timer), everything else spawned on demand and reclaimed after sitting
idle past retention_seconds.
"""

import logging
import threading
import time
from dataclasses import dataclass
from typing import Optional

from .docker_runner import cleanup_orphans, create_container, is_alive, wipe_and_health_check

logger = logging.getLogger("spawner.pool")


class PoolExhausted(Exception):
    """Raised when every container is busy and max_containers is already hit.
    Callers must treat this as backpressure: leave the job unacked so the broker
    retries it later, never spawn past the cap."""


@dataclass
class PoolEntry:
    container: object
    state: str  # "idle" | "busy"
    last_used: float
    is_floor: bool = False


class Pool:
    def __init__(self, client, settings, container_factory=None):
        self._client = client
        self._settings = settings
        self._lock = threading.Lock()
        self._entries: dict[str, PoolEntry] = {}
        self._floor_id: Optional[str] = None
        self._stop = threading.Event()
        # Injectable so tests can run this without a real Docker daemon.
        self._container_factory = container_factory or (
            lambda: create_container(
                client, settings.sandbox_image, settings.container_memory_mb, settings.container_cpus
            )
        )

    def start(self):
        # A restart (crash, Docker pausing/restarting, a redeploy) loses all
        # in-memory tracking of whatever the previous process instance
        # created. Without this, those containers become permanently
        # invisible orphans: never reused, never reaped by the retention
        # timer, running forever until someone removes them by hand. This was
        # a real bug, found by noticing containers far older than the
        # retention window still running. Skipped when a fake client is
        # injected for testing.
        if self._client is not None:
            cleanup_orphans(self._client)
        self._ensure_floor()
        threading.Thread(target=self._reaper_loop, daemon=True).start()
        threading.Thread(target=self._floor_supervisor_loop, daemon=True).start()

    def stop(self):
        self._stop.set()

    def claim(self):
        """Atomically hands back an idle container, or spawns a new one if none is
        idle. The lock is what stops two jobs grabbing the same idle container at
        the same instant (see the race-condition failure case in the design doc)."""
        with self._lock:
            for cid, entry in self._entries.items():
                if entry.state == "idle":
                    entry.state = "busy"
                    return cid, entry
            if len(self._entries) >= self._settings.max_containers:
                raise PoolExhausted()

        # Spawning is slow I/O; do it outside the lock so one spawn can't block
        # every other claim attempt.
        container = self._container_factory()
        entry = PoolEntry(container=container, state="busy", last_used=time.time())
        with self._lock:
            self._entries[container.id] = entry
        return container.id, entry

    def release(self, cid: str, healthy: bool):
        """healthy=False means the run ended abnormally (OOM, sandbox violation,
        an exec that raised): destroy rather than reuse, even if a wipe would
        technically succeed. This is a deliberate rule, not just an optimization,
        see "Additional Trade-offs" / reuse-safety in the design doc."""
        entry = self._entries.get(cid)
        if healthy and entry is not None and wipe_and_health_check(entry.container):
            with self._lock:
                entry.state = "idle"
                entry.last_used = time.time()
            return
        self._destroy(cid)

    def stats(self) -> dict:
        with self._lock:
            idle = sum(1 for e in self._entries.values() if e.state == "idle")
            busy = sum(1 for e in self._entries.values() if e.state == "busy")
            return {
                "total": len(self._entries),
                "idle": idle,
                "busy": busy,
                "floor_healthy": self._floor_id in self._entries,
            }

    def _ensure_floor(self):
        with self._lock:
            if self._floor_id is not None and self._floor_id in self._entries:
                return
        container = self._container_factory()
        with self._lock:
            self._entries[container.id] = PoolEntry(
                container=container, state="idle", last_used=time.time(), is_floor=True
            )
            self._floor_id = container.id

    def _destroy(self, cid: str):
        with self._lock:
            entry = self._entries.pop(cid, None)
            if entry is not None and entry.is_floor:
                self._floor_id = None
        if entry is None:
            return
        try:
            entry.container.remove(force=True)
        except Exception:
            pass
        if entry.is_floor:
            self._ensure_floor()

    def _reaper_loop(self):
        """Simple periodic sweep rather than one timer per container: if a sweep
        is missed, the next tick just picks up from reality, nothing is lost.
        The try/except is load-bearing, not decoration: a transient Docker
        failure (daemon paused/restarting) must not permanently kill this
        thread, or every container leaks forever until the service is
        restarted. This was a real bug, found when Docker Desktop's idle
        auto-pause killed the thread mid-run; it now just logs and retries
        on the next tick instead."""
        while not self._stop.is_set():
            time.sleep(self._settings.reaper_interval_seconds)
            try:
                now = time.time()
                with self._lock:
                    expired = [
                        cid
                        for cid, e in self._entries.items()
                        if not e.is_floor
                        and e.state == "idle"
                        and now - e.last_used > self._settings.retention_seconds
                    ]
                for cid in expired:
                    self._destroy(cid)
            except Exception:
                logger.exception("reaper sweep failed, will retry next tick")

    def _floor_supervisor_loop(self):
        """Continuously enforces the '1 always open' invariant, not just at
        boot. Same reasoning as the reaper: one failed attempt to respawn the
        floor must not kill this loop forever, or the floor never comes back
        even after Docker recovers."""
        while not self._stop.is_set():
            time.sleep(self._settings.reaper_interval_seconds)
            try:
                with self._lock:
                    fid = self._floor_id
                    entry = self._entries.get(fid) if fid else None
                if entry is None:
                    self._ensure_floor()
                elif not is_alive(entry.container):
                    self._destroy(fid)
            except Exception:
                logger.exception("floor supervisor tick failed, will retry next tick")
