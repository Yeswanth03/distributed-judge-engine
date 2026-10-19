package com.minicodejudge.catalog.controller;

import com.minicodejudge.catalog.dto.TestsetDtos.TestsetCreated;
import com.minicodejudge.catalog.dto.TestsetDtos.TestsetRequest;
import com.minicodejudge.catalog.service.TestsetService;
import jakarta.validation.Valid;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/** Admin writes for a problem's test sets. The admin role check lives in AuthInterceptor. */
@RestController
@RequestMapping("/admin/problems/{id}/testsets")
public class AdminTestsetController {

    private final TestsetService service;

    public AdminTestsetController(TestsetService service) {
        this.service = service;
    }

    @PostMapping
    public ResponseEntity<TestsetCreated> upload(@PathVariable("id") UUID id, @Valid @RequestBody TestsetRequest body) {
        return ResponseEntity.status(HttpStatus.CREATED).body(service.upload(id, body));
    }

    @PostMapping("/{version}/publish")
    public ResponseEntity<Void> publish(@PathVariable("id") UUID id, @PathVariable("version") int version) {
        service.publish(id, version);
        return ResponseEntity.noContent().build();
    }
}
