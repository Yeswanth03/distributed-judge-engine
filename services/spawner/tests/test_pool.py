"""Unit tests for the pool manager, using a fake container so these run without a
real Docker daemon. These exercise the behaviors from the design doc directly:
floor-on-start, atomic claim, reuse-vs-destroy, retention reaping, and the max cap.
"""

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
    loop, and it does recover on a later tick."""
    first_floor = next(iter(pool._entries.values())).container
    first_floor.removed = True  # floor dies, as if the host crashed it

    real_factory = pool._container_factory
    attempts = {"n": 0}

    def flaky_factory():
        attempts["n"] += 1
        if attempts["n"] == 1:
            raise RuntimeError("docker daemon unreachable (simulated)")
        return real_factory()

    monkeypatch.setattr(pool, "_container_factory", flaky_factory)

    time.sleep(0.1)  # supervisor tick 1: tries to respawn, fails, must not crash the loop
    assert pool.stats()["floor_healthy"] is False

    time.sleep(0.1)  # supervisor tick 2: should succeed now that the factory recovered
    stats = pool.stats()
    assert stats["total"] == 1
    assert stats["floor_healthy"] is True


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
