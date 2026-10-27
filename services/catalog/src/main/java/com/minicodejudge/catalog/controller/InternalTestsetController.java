package com.minicodejudge.catalog.controller;

import com.minicodejudge.catalog.config.OpenApiConfig;
import com.minicodejudge.catalog.dto.ApiError;
import com.minicodejudge.catalog.dto.InternalDtos.InternalTestset;
import com.minicodejudge.catalog.service.TestsetService;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.Parameter;
import io.swagger.v3.oas.annotations.headers.Header;
import io.swagger.v3.oas.annotations.media.Content;
import io.swagger.v3.oas.annotations.media.ExampleObject;
import io.swagger.v3.oas.annotations.media.Schema;
import io.swagger.v3.oas.annotations.responses.ApiResponse;
import io.swagger.v3.oas.annotations.security.SecurityRequirement;
import io.swagger.v3.oas.annotations.tags.Tag;
import java.time.Duration;
import java.util.UUID;
import org.jspecify.annotations.Nullable;
import org.springframework.http.CacheControl;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

/** Called by judge workers only, with the service token (checked in AuthInterceptor). Never routed by the gateway. */
@RestController
@Tag(name = "Internal (judge)", description = "Service-to-service. Needs X-Service-Token; never exposed through the gateway.")
@SecurityRequirement(name = OpenApiConfig.SERVICE_TOKEN)
public class InternalTestsetController {

    /** A published version never changes, so a response for an explicit version may be cached indefinitely. */
    private static final CacheControl FOREVER = CacheControl.maxAge(Duration.ofDays(365)).cachePublic().immutable();

    private final TestsetService service;

    public InternalTestsetController(TestsetService service) {
        this.service = service;
    }

    @GetMapping("/internal/problems/{id}/testset")
    @Operation(summary = "Get a published test set for judging",
            description = "All cases (hidden and sample) in judging order, plus the limits to enforce. With version: "
                    + "that exact published version, cacheable forever. Without: the current version, not cacheable "
                    + "because it moves when a newer version is published. Drafts are never served.")
    @ApiResponse(responseCode = "200", description = "The test set", headers = @Header(name = "Cache-Control",
            description = "public, max-age=31536000, immutable with an explicit version; no-cache without one"))
    @ApiResponse(responseCode = "400", description = "id is not a UUID or version is not a number", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"bad_request\",\"message\":\"invalid value for id\"}")))
    @ApiResponse(responseCode = "401", description = "Missing or wrong service token", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"unauthorized\",\"message\":\"valid service token required\"}")))
    @ApiResponse(responseCode = "404", description = "Unknown problem, nothing published yet, or version missing or still a draft",
            content = @Content(schema = @Schema(implementation = ApiError.class), examples = @ExampleObject(
                    value = "{\"error\":\"not_found\",\"message\":\"test set version 2 is not published\"}")))
    public ResponseEntity<InternalTestset> testset(
            @Parameter(description = "Problem id (from the job)") @PathVariable("id") UUID id,
            @Parameter(description = "Published version; omit for the current one", example = "1")
            @RequestParam(name = "version", required = false) @Nullable Integer version) {
        InternalTestset body = service.forJudge(id, version);
        CacheControl cache = version != null ? FOREVER : CacheControl.noCache();
        return ResponseEntity.ok().cacheControl(cache).body(body);
    }
}
