package com.minicodejudge.catalog.controller;

import com.minicodejudge.catalog.dto.ApiError;
import com.minicodejudge.catalog.dto.LeaderboardDtos.Leaderboard;
import com.minicodejudge.catalog.dto.LeaderboardDtos.UserStanding;
import com.minicodejudge.catalog.service.LeaderboardService;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.Parameter;
import io.swagger.v3.oas.annotations.media.Content;
import io.swagger.v3.oas.annotations.media.ExampleObject;
import io.swagger.v3.oas.annotations.media.Schema;
import io.swagger.v3.oas.annotations.responses.ApiResponse;
import io.swagger.v3.oas.annotations.security.SecurityRequirements;
import io.swagger.v3.oas.annotations.tags.Tag;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

/** Public leaderboard reads. Eventually consistent: every response says how fresh it is. */
@RestController
@RequestMapping("/leaderboard")
@Tag(name = "Leaderboard", description = "Public reads. Built asynchronously from verdict events, so it may trail "
        + "the newest verdicts slightly; as_of and lag_seconds say by how much.")
@SecurityRequirements // public: overrides the global admin-role requirement
public class LeaderboardController {

    private final LeaderboardService service;

    public LeaderboardController(LeaderboardService service) {
        this.service = service;
    }

    @GetMapping
    @Operation(summary = "Top users",
            description = "Users with at least one solve, by score. On equal score, whoever reached it earlier ranks higher.")
    @ApiResponse(responseCode = "200", description = "The top of the leaderboard")
    @ApiResponse(responseCode = "400", description = "limit outside 1 to 100", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"bad_request\",\"message\":\"limit must be less than or equal to 100\"}")))
    public Leaderboard top(
            @Parameter(description = "How many users, 1 to 100")
            @RequestParam(name = "limit", defaultValue = "50") @Min(1) @Max(100) int limit) {
        return service.top(limit);
    }

    @GetMapping("/users/{userId}")
    @Operation(summary = "One user's standing", description = "Rank, score and solved problems. rank is null until the first solve.")
    @ApiResponse(responseCode = "200", description = "The user's standing")
    @ApiResponse(responseCode = "404", description = "No verdict has been received for this user", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"not_found\",\"message\":\"no verdicts recorded for this user\"}")))
    public UserStanding standing(@Parameter(example = "user-42") @PathVariable("userId") String userId) {
        return service.standing(userId);
    }
}
