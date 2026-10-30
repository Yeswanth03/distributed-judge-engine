"""Unit tests for the pool manager, using a fake container so these run without a
real Docker daemon. These exercise the behaviors from the design doc directly:
floor-on-start, atomic claim, reuse-vs-destroy, retention reaping, and the max cap.
"""

import threading
import time

import pytest

from app.pool import Pool, PoolExhausted


class FakeContainer:
    def __init__(self, cid):
        self.id = cid
        self.removed = False

    def remove(self, force=True):
        self.removed = True


class FakeSettings:
    sandbox_image = "fake:image"
    floor_size = 1
    retention_seconds = 0.2
    reaper_interval_seconds = 0.05
    max_containers = 2
    container_memory_mb = 256
    container_cpus = 1.0


@pytest.fixture
def pool(monkeypatch):
    counter = {"n": 0}

    def factory():
        counter["n"] += 1
        return FakeContainer(f"c{counter['n']}")

    monkeypatch.setattr("app.pool.is_alive", lambda c: not c.removed)
    monkeypatch.setattr("app.pool.wipe_and_health_check", lambda c: not c.removed)

    p = Pool(client=None, settings=FakeSettings(), container_factory=factory)
    p.start()
    time.sleep(0.05)
    yield p
    p.stop()
    time.sleep(0.1)  # let the background loops see _stop before monkeypatch reverts


def test_start_cleans_up_orphaned_containers_from_a_previous_instance(monkeypatch):
    """Regression test for a real bug: a restarted Spawner process has no
    memory of what a previous instance created, so those containers became
    permanently invisible orphans, never reused, never reaped by the
    retention timer. Found by noticing containers older than the retention
    window still running on the host. Confirms start() removes anything
    matching our naming prefix before creating a fresh floor."""
    removed = []

    class FakeOrphan:
        def remove(self, force=True):
            removed.append(True)

    class FakeContainersAPI:
        def list(self, all=True, filters=None):
            assert filters == {"name": "spawner-sandbox-"}
            return [FakeOrphan(), FakeOrphan()]

    class FakeClient:
        containers = FakeContainersAPI()

    counter = {"n": 0}

    def factory():
        counter["n"] += 1
        return FakeContainer(f"c{counter['n']}")

    monkeypatch.setattr("app.pool.is_alive", lambda c: not c.removed)
    monkeypatch.setattr("app.pool.wipe_and_health_check", lambda c: not c.removed)

    p = Pool(client=FakeClient(), settings=FakeSettings(), container_factory=factory)
    p.start()
    time.sleep(0.05)
    p.stop()
    time.sleep(0.1)

    assert len(removed) == 2  # both orphans from the "previous instance" were cleaned up


def test_floor_container_spawned_on_start(pool):
    stats = pool.stats()
    assert stats["total"] == 1
    assert stats["floor_healthy"] is True


def test_claim_reuses_idle_then_spawns_new(pool):
    cid1, _ = pool.claim()  # takes the floor container
    cid2, _ = pool.claim()  # floor is busy now, spawns a new one
    assert cid1 != cid2
    assert pool.stats()["busy"] == 2


def test_pool_exhausted_past_cap(pool):
    pool.claim()
    pool.claim()  # both of max_containers=2 are now busy
    with pytest.raises(PoolExhausted):
        pool.claim()


def test_unhealthy_release_destroys_instead_of_reusing(pool):
    pool.claim()  # takes the floor container, left busy
    cid2, _ = pool.claim()  # floor's busy, spawns a second, on-demand container
    pool.release(cid2, healthy=False)
    stats = pool.stats()
    assert stats["total"] == 1  # the on-demand container is gone, only floor remains
    assert stats["busy"] == 1  # the first claim (the floor) is still held


def test_idle_non_floor_container_reaped_after_retention(pool):
    cid1, _ = pool.claim()
    cid2, _ = pool.claim()
    pool.release(cid1, healthy=True)
    pool.release(cid2, healthy=True)
    time.sleep(0.4)  # longer than retention_seconds
    stats = pool.stats()
    assert stats["total"] == 1  # only the floor container survives
    assert stats["floor_healthy"] is True


def test_floor_supervisor_survives_a_transient_spawn_failure(monkeypatch, pool):
    """Regression test for a real bug: Docker Desktop's idle auto-pause made
    container creation fail transiently, and the supervisor loop had no
    try/except, so it died permanently and the floor never came back even
    after Docker recovered. This confirms one failed attempt doesn't kill the
    loop, and it does recover on a later tick.

    Polls instead of sleeping for a fixed duration: a fixed sleep has to
    guess how many ticks land inside it, and with a 0.05s tick interval a
    0.1s sleep is right on the boundary, sometimes one tick fits, sometimes
    two, which made this test genuinely flaky, not just slow."""
    first_floor = next(iter(pool._entries.values())).container
    first_floor.removed = True  # floor dies, as if the host crashed it

    real_factory = pool._container_factory
    attempts = {"n": 0}
    allow_success = threading.Event()

    def flaky_factory():
        attempts["n"] += 1
        if attempts["n"] == 1:
            raise RuntimeError("docker daemon unreachable (simulated)")
        allow_success.wait(timeout=2)  # don't succeed until the test says to
        return real_factory()

    monkeypatch.setattr(pool, "_container_factory", flaky_factory)

    _wait_until(lambda: attempts["n"] >= 1)
    # The first (failing) attempt must not have crashed the supervisor loop,
    # and must not have left a stale floor behind.
    _wait_until(lambda: pool.stats()["floor_healthy"] is False)

    allow_success.set()  # now let the next attempt succeed
    _wait_until(lambda: pool.stats()["floor_healthy"] is True)
    assert pool.stats()["total"] == 1


def _wait_until(condition, timeout=2.0, interval=0.01):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if condition():
            return
        time.sleep(interval)
    raise AssertionError(f"condition not met within {timeout}s")


def test_floor_respawns_if_it_dies(pool):
    stats = pool.stats()
    assert stats["total"] == 1
    # Kill the floor container out from under the pool, as if the host crashed it.
    first_floor = next(iter(pool._entries.values())).container
    first_floor.removed = True
    time.sleep(0.15)  # give the supervisor loop a tick to notice and respawn
    stats = pool.stats()
    assert stats["total"] == 1
    assert stats["floor_healthy"] is True
