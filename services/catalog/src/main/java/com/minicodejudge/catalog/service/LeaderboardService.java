package com.minicodejudge.catalog.service;

import com.minicodejudge.catalog.dto.LeaderboardDtos.Leaderboard;
import com.minicodejudge.catalog.dto.LeaderboardDtos.LeaderboardEntry;
import com.minicodejudge.catalog.dto.LeaderboardDtos.UserStanding;
import com.minicodejudge.catalog.messaging.VerdictEvent;
import com.minicodejudge.catalog.repository.LeaderboardRepository;
import com.minicodejudge.catalog.repository.LeaderboardRepository.Freshness;
import com.minicodejudge.catalog.repository.LeaderboardRepository.Standing;
import java.time.Duration;
import java.time.Instant;
import java.util.List;
import java.util.Optional;
import org.jspecify.annotations.Nullable;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/**
 * Leaderboard rules. Verdicts are applied by recomputing from stored facts, never by adding to a running total,
 * so duplicate deliveries, out-of-order messages and rejudges all converge to the same scores.
 */
@Service
public class LeaderboardService {

    private final LeaderboardRepository repo;

    public LeaderboardService(LeaderboardRepository repo) {
        this.repo = repo;
    }

    /**
     * Applies one verdict in a single transaction; the message is acknowledged only after it commits.
     * rejudge_seq decides: higher replaces the stored verdict, equal is a duplicate, lower is stale (both ignored).
     *
     * @return true if the leaderboard changed, false for a duplicate or stale message
     */
    @Transactional
    public boolean apply(VerdictEvent event) {
        if (!repo.problemExists(event.problemId())) {
            throw new InvalidVerdictException("unknown problem " + event.problemId());
        }
        repo.lockUser(event.userId());
        if (repo.upsertResult(event) == 0) {
            return false;
        }
        repo.recomputeStatus(event.userId(), event.problemId());
        repo.recomputeScore(event.userId());
        return true;
    }

    public Leaderboard top(int limit) {
        Optional<Freshness> freshness = repo.latestApplied();
        List<LeaderboardEntry> entries = repo.top(limit).stream()
                .map(r -> new LeaderboardEntry(r.rank(), r.userId(), r.score(), r.solvedCount()))
                .toList();
        return new Leaderboard(asOf(freshness), lagSeconds(freshness), entries);
    }

    public UserStanding standing(String userId) {
        Standing s = repo.standing(userId)
                .orElseThrow(() -> new NotFoundException("no verdicts recorded for this user"));
        Optional<Freshness> freshness = repo.latestApplied();
        return new UserStanding(s.userId(), s.rank(), s.score(), s.solvedCount(), repo.solvedProblemIds(userId),
                asOf(freshness), lagSeconds(freshness));
    }

    /** The leaderboard reflects verdicts judged up to this time. */
    private static @Nullable Instant asOf(Optional<Freshness> f) {
        return f.map(Freshness::judgedAt).orElse(null);
    }

    /** How long the newest verdict took from being judged to being applied. */
    private static @Nullable Double lagSeconds(Optional<Freshness> f) {
        return f.map(x -> Math.max(0, Duration.between(x.judgedAt(), x.appliedAt()).toMillis()) / 1000.0).orElse(null);
    }
}
