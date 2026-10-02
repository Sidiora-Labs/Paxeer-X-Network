#include "layerx/lxp_daemon.h"
#include "layerx/lxp_crypto.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static void record_outstanding_locked(lxp_daemon *daemon)
{
    size_t index;
    size_t durable = 0U;
    uint64_t first = 0U;
    if (daemon->queue_count == 0U) return;
    if (lxp_daemon_queue_sequence_locked(daemon, 0U, &first) != LXP_OK)
        first = daemon->next_sequence;
    for (index = 0U; index < daemon->queue_count; ++index) {
        size_t at = (daemon->queue_head + index) % LXP_DAEMON_QUEUE_CAPACITY;
        if (daemon->queue[at].durable_admission) ++durable;
        if (daemon->queue[at].bytes != NULL) {
            lxp_secure_zero(daemon->queue[at].bytes, daemon->queue[at].length);
            free(daemon->queue[at].bytes);
        }
        daemon->queue[at].bytes = NULL;
        daemon->queue[at].length = 0U;
        (void)memset(daemon->queue[at].activity_id, 0,
                     sizeof(daemon->queue[at].activity_id));
        daemon->queue[at].global_sequence = 0U;
        daemon->queue[at].durable_admission = false;
    }
    (void)fprintf(stderr,
                  "layerxd: shutdown recorded outstanding=%zu durable=%zu "
                  "first_sequence=%llu\n",
                  daemon->queue_count, durable, (unsigned long long)first);
    daemon->queue_head = 0U;
    daemon->queue_count = 0U;
    daemon->queue_bytes = 0U;
    daemon->reserved_batch_count = 0U;
}

lxp_result lxp_daemon_shutdown(lxp_daemon *daemon)
{
    size_t joined = 0U;
    lxp_result status;
    lxp_result join_status = LXP_OK;
    if (daemon == NULL || !daemon->primitives_initialized)
        return LXP_ERR_NON_CANONICAL;
    (void)pthread_mutex_lock(&daemon->mutex);
    daemon->accepting = false;
    daemon->stop_requested = true;
    (void)pthread_cond_broadcast(&daemon->queue_changed);
    (void)pthread_mutex_unlock(&daemon->mutex);
    if (daemon->executor_started) {
        if (pthread_join(daemon->executor_thread, NULL) == 0) ++joined;
        else join_status = LXP_ERR_IO;
        daemon->executor_started = false;
    }
    (void)pthread_mutex_lock(&daemon->mutex);
    record_outstanding_locked(daemon);
    status = daemon->failure;
    (void)fprintf(stderr,
                  "layerxd: shutdown joined_threads=%zu executed=%llu "
                  "next_sequence=%llu result=%d\n",
                  joined, (unsigned long long)daemon->executed_count,
                  (unsigned long long)daemon->next_sequence,
                  (int)status);
    (void)pthread_mutex_unlock(&daemon->mutex);
    if (status == LXP_OK) status = join_status;
    if (daemon->protocol_owner != NULL) {
        lxp_result owner_status = lxp_daemon_protocol_listener_stop(
            daemon->protocol_owner);
        if (owner_status == LXP_OK)
            owner_status = lxp_daemon_protocol_owner_detach(
                daemon->protocol_owner);
        if (status == LXP_OK) status = owner_status;
        daemon->protocol_owner = NULL;
    }
    (void)pthread_cond_destroy(&daemon->queue_changed);
    (void)pthread_mutex_destroy(&daemon->mutex);
    daemon->primitives_initialized = false;
    return status;
}
