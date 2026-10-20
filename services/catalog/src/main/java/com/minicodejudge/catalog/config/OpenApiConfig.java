package com.minicodejudge.catalog.config;

import com.fasterxml.jackson.databind.PropertyNamingStrategies;
import io.swagger.v3.core.jackson.ModelResolver;
import io.swagger.v3.core.util.Json;
import io.swagger.v3.oas.models.Components;
import io.swagger.v3.oas.models.OpenAPI;
import io.swagger.v3.oas.models.info.Info;
import io.swagger.v3.oas.models.security.SecurityRequirement;
import io.swagger.v3.oas.models.security.SecurityScheme;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

/**
 * The OpenAPI document behind Swagger UI. The admin role is declared as a header "security scheme", so Swagger's
 * Authorize button can set X-User-Role (normally added by the gateway) on every request.
 */
@Configuration
public class OpenApiConfig {

    private static final String ADMIN_ROLE = "adminRole";

    @Bean
    OpenAPI catalogOpenApi() {
        return new OpenAPI()
                .info(new Info()
                        .title("Catalog service")
                        .version("0.1.0")
                        .description("""
                                Problems and versioned test sets. All JSON fields are snake_case.

                                Click **Authorize** and enter `admin` (the gateway normally sets X-User-Role).
                                Typical flow: create a problem → upload a test set → publish that version.
                                """))
                .components(new Components().addSecuritySchemes(ADMIN_ROLE, new SecurityScheme()
                        .type(SecurityScheme.Type.APIKEY)
                        .in(SecurityScheme.In.HEADER)
                        .name("X-User-Role")
                        .description("Enter: admin")))
                .addSecurityItem(new SecurityRequirement().addList(ADMIN_ROLE));
    }

    /**
     * Swagger reads models with its own Jackson 2 mapper, which ignores spring.jackson.* (that configures the
     * app's Jackson 3 mapper). Without this the docs would show statementMd while the API only accepts statement_md.
     */
    @Bean
    ModelResolver snakeCaseModelResolver() {
        return new ModelResolver(Json.mapper().copy().setPropertyNamingStrategy(PropertyNamingStrategies.SNAKE_CASE));
    }
}
