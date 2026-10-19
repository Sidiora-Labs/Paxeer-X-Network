package com.sidiora.layerx.android;

import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.function.LongSupplier;

/** Process-local delivery ledger for a single application session. */
public final class InMemoryEventDeliveryStore implements EventDeliveryStore {
    private static final class Entry {
        private final String payloadDigest;
        private long leaseUntilMs;
        private boolean completed;

        private Entry(String payloadDigest, long leaseUntilMs) {
            this.payloadDigest = payloadDigest;
            this.leaseUntilMs = leaseUntilMs;
        }
    }

    private final Map<String, Entry> entries = new LinkedHashMap<>();
    private final int capacity;
    private final LongSupplier clock;

    public InMemoryEventDeliveryStore() {
        this(8_192, null);
    }

    public InMemoryEventDeliveryStore(int capacity, LongSupplier clock) {
        this.capacity = Math.max(capacity, 1);
        this.clock = clock == null ? System::currentTimeMillis : clock;
    }

    @Override
    public synchronized Claim claim(String deliveryId, String payloadDigest, long leaseUntilMs) {
        Entry existing = entries.get(deliveryId);
        if (existing != null) {
            if (!existing.payloadDigest.equals(payloadDigest)) return Claim.CONFLICT;
            if (existing.completed) return Claim.COMPLETED;
            if (existing.leaseUntilMs > clock.getAsLong()) return Claim.PROCESSING;
            existing.leaseUntilMs = leaseUntilMs;
            return Claim.CLAIMED;
        }
        if (entries.size() >= capacity) evict();
        entries.put(deliveryId, new Entry(payloadDigest, leaseUntilMs));
        return Claim.CLAIMED;
    }

    @Override
    public synchronized void complete(String deliveryId, String payloadDigest) {
        Entry entry = entries.get(deliveryId);
        if (entry == null || !entry.payloadDigest.equals(payloadDigest)) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.EVENT_REPLAY);
        }
        entry.completed = true;
        entry.leaseUntilMs = 0L;
    }

    @Override
    public synchronized void release(String deliveryId, String payloadDigest) {
        Entry entry = entries.get(deliveryId);
        if (entry == null || !entry.payloadDigest.equals(payloadDigest) || entry.completed) return;
        entries.remove(deliveryId);
    }

    private void evict() {
        long now = clock.getAsLong();
        Iterator<Map.Entry<String, Entry>> iterator = entries.entrySet().iterator();
        while (iterator.hasNext()) {
            Entry entry = iterator.next().getValue();
            if (entry.completed || entry.leaseUntilMs <= now) iterator.remove();
        }
        if (entries.size() >= capacity) {
            Iterator<String> oldest = entries.keySet().iterator();
            if (oldest.hasNext()) {
                oldest.next();
                oldest.remove();
            }
        }
    }
}
