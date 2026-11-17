package com.minicodejudge.catalog.messaging;

/**
 * A user's public state, as the leaderboard needs it. Built from a User Service event by {@link UserEventParser},
 * the only place that knows the event's wire format.
 */
public record UserEvent(String userId, String username, String displayName, String status, long version) {}
