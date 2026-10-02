#define _POSIX_C_SOURCE 200809L

#include "layerx/lxp_storage.h"

#include <stdint.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

static int run_child(const char *directory, uint32_t abort_boundary)
{
    const uint8_t activity[] = { 1U, 2U };
    const uint8_t receipt[] = { 3U, 4U };
    const uint8_t state_diff[] = { 5U, 6U };
    const uint8_t batch[] = { 7U, 8U };
    lxp_log log;
    if (lxp_log_segment_create(&log, directory, 0U, 4096U) != LXP_OK)
        return 1;
    if (lxp_log_append(&log, LXP_LOG_ACTIVITY, 11U, activity,
                       (uint32_t)sizeof(activity), NULL) != LXP_OK ||
        lxp_log_write_boundary(&log) != LXP_OK) return 1;
    if (lxp_log_fault_point(1U, abort_boundary)) _exit(81);
    if (lxp_log_append(&log, LXP_LOG_RECEIPT, 11U, receipt,
                       (uint32_t)sizeof(receipt), NULL) != LXP_OK ||
        lxp_log_append(&log, LXP_LOG_STATE_DIFF, 11U, state_diff,
                       (uint32_t)sizeof(state_diff), NULL) != LXP_OK ||
        lxp_log_write_boundary(&log) != LXP_OK) return 1;
    if (lxp_log_fault_point(2U, abort_boundary)) _exit(82);
    if (lxp_log_append(&log, LXP_LOG_BATCH_HEADER, 11U, batch,
                       (uint32_t)sizeof(batch), NULL) != LXP_OK ||
        lxp_log_write_boundary(&log) != LXP_OK) return 1;
    if (lxp_log_fault_point(3U, abort_boundary)) _exit(83);
    return lxp_log_close(&log) == LXP_OK ? 0 : 1;
}

#define LXP_RESTART_RECORD_BYTES ((uint64_t)LXP_LOG_HEADER_BYTES + 2U)

typedef struct restart_replay {
    uint32_t records;
    uint8_t first_kind;
} restart_replay;

static lxp_result count_replay(void *context,
                               const lxp_log_record_header *header,
                               const uint8_t *body)
{
    restart_replay *replay = (restart_replay *)context;
    (void)body;
    if (replay->records == 0U) replay->first_kind = header->record_kind;
    ++replay->records;
    return LXP_OK;
}

static int run_canonical_child(const char *directory, uint32_t abort_boundary)
{
    const uint8_t first[] = { 21U, 1U };
    const uint8_t checkpoint[] = { 21U, 4U };
    const uint8_t second[] = { 22U, 1U };
    const uint8_t pending[] = { 23U, 1U };
    lxp_log log;
    if (lxp_log_segment_create(&log, directory, 0U, 4096U) != LXP_OK)
        return 1;
    if (lxp_log_append(&log, LXP_LOG_ACTIVITY, 21U, first,
                       (uint32_t)sizeof(first), NULL) != LXP_OK ||
        lxp_log_append(&log, LXP_LOG_RECEIPT, 21U, first,
                       (uint32_t)sizeof(first), NULL) != LXP_OK ||
        lxp_log_write_boundary(&log) != LXP_OK) return 1;
    if (lxp_log_fault_point(1U, abort_boundary)) _exit(91);
    if (lxp_log_append(&log, LXP_LOG_CHECKPOINT, 21U, checkpoint,
                       (uint32_t)sizeof(checkpoint), NULL) != LXP_OK ||
        lxp_log_write_boundary(&log) != LXP_OK) return 1;
    if (lxp_log_fault_point(2U, abort_boundary)) _exit(92);
    if (lxp_log_append(&log, LXP_LOG_ACTIVITY, 22U, second,
                       (uint32_t)sizeof(second), NULL) != LXP_OK ||
        lxp_log_append(&log, LXP_LOG_RECEIPT, 22U, second,
                       (uint32_t)sizeof(second), NULL) != LXP_OK ||
        lxp_log_write_boundary(&log) != LXP_OK) return 1;
    if (lxp_log_fault_point(3U, abort_boundary)) _exit(93);
    if (lxp_log_append(&log, LXP_LOG_ACTIVITY, 23U, pending,
                       (uint32_t)sizeof(pending), NULL) != LXP_OK) return 1;
    if (lxp_log_fault_point(4U, abort_boundary)) _exit(94);
    return 1;
}

static int run_batch_child(const char *directory, uint32_t abort_boundary)
{
    const uint8_t header[] = { 5U, 3U };
    const uint8_t body[] = { 5U, 9U };
    const uint8_t later_header[] = { 6U, 3U };
    const uint8_t later_body[] = { 6U, 9U };
    const uint8_t pending[] = { 7U, 3U };
    lxp_log log;
    if (lxp_log_segment_create(&log, directory, 0U, 4096U) != LXP_OK)
        return 1;
    if (lxp_log_append(&log, LXP_LOG_BATCH_HEADER, 5U, header,
                       (uint32_t)sizeof(header), NULL) != LXP_OK ||
        lxp_log_append(&log, LXP_LOG_BATCH_BODY, 5U, body,
                       (uint32_t)sizeof(body), NULL) != LXP_OK ||
        lxp_log_write_boundary(&log) != LXP_OK) return 1;
    if (lxp_log_fault_point(1U, abort_boundary)) _exit(71);
    if (lxp_log_append(&log, LXP_LOG_BATCH_HEADER, 6U, later_header,
                       (uint32_t)sizeof(later_header), NULL) != LXP_OK ||
        lxp_log_append(&log, LXP_LOG_BATCH_BODY, 6U, later_body,
                       (uint32_t)sizeof(later_body), NULL) != LXP_OK ||
        lxp_log_write_boundary(&log) != LXP_OK) return 1;
    if (lxp_log_fault_point(2U, abort_boundary)) _exit(72);
    if (lxp_log_append(&log, LXP_LOG_BATCH_HEADER, 7U, pending,
                       (uint32_t)sizeof(pending), NULL) != LXP_OK) return 1;
    if (lxp_log_fault_point(3U, abort_boundary)) _exit(73);
    return 1;
}

static int crash_child(const char *directory, uint32_t boundary, int batch,
                       int expected_exit)
{
    pid_t child;
    int child_status;
    child = fork();
    if (child < 0) return 1;
    if (child == 0)
        _exit(batch ? run_batch_child(directory, boundary) :
                      run_canonical_child(directory, boundary));
    if (waitpid(child, &child_status, 0) != child ||
        !WIFEXITED(child_status) ||
        WEXITSTATUS(child_status) != expected_exit)
        return 1;
    return 0;
}

static int recover_restart(const char *path, int complete_records,
                           uint64_t expected_end, uint64_t expected_next,
                           uint64_t expected_head, uint32_t expected_replay,
                           uint8_t expected_first_kind)
{
    lxp_log log;
    restart_replay replay = { 0U, 0U };
    uint64_t durable;
    lxp_result status;
    if (lxp_log_open(&log, path) != LXP_OK) return 1;
    if (!log.has_durable_marker || log.durable_offset != expected_end ||
        log.durable_next_sequence != expected_next) {
        (void)lxp_log_close(&log);
        return 1;
    }
    status = complete_records ?
        lxp_log_recover_complete_records(&log, count_replay, &replay) :
        lxp_log_recover(&log, count_replay, &replay);
    if (status != LXP_OK || log.write_offset == 0U ||
        log.write_offset != expected_end ||
        lxp_log_resume_sequence(&log) != expected_next ||
        replay.records != expected_replay ||
        replay.first_kind != expected_first_kind ||
        lxp_log_durable_head(&log, &durable) != LXP_OK ||
        durable != expected_head) {
        (void)lxp_log_close(&log);
        return 1;
    }
    return lxp_log_close(&log) == LXP_OK ? 0 : 1;
}

static int append_after_restart(const char *path, int complete_records,
                                int batch, uint64_t expected_end,
                                uint64_t expected_next)
{
    const uint8_t bytes[] = { 31U, 7U };
    lxp_log log;
    lxp_result status;
    if (lxp_log_open(&log, path) != LXP_OK) return 1;
    status = complete_records ? lxp_log_recover_complete_records(&log, NULL, NULL) :
                                lxp_log_recover(&log, NULL, NULL);
    if (status != LXP_OK || log.write_offset != expected_end ||
        lxp_log_resume_sequence(&log) != expected_next ||
        lxp_log_append(&log, batch ? LXP_LOG_BATCH_HEADER : LXP_LOG_ACTIVITY,
                       expected_next, bytes, (uint32_t)sizeof(bytes),
                       NULL) != LXP_OK ||
        (!batch && lxp_log_append(&log, LXP_LOG_RECEIPT, expected_next, bytes,
                                  (uint32_t)sizeof(bytes), NULL) != LXP_OK) ||
        lxp_log_write_boundary(&log) != LXP_OK) {
        (void)lxp_log_close(&log);
        return 1;
    }
    return lxp_log_close(&log) == LXP_OK ? 0 : 1;
}

static int refuse_restart(const char *path, int complete_records,
                          lxp_result expected, int close_descriptor)
{
    lxp_log log;
    lxp_result status;
    if (lxp_log_open(&log, path) != LXP_OK) return 1;
    if (close_descriptor && close(log.descriptor) != 0) return 1;
    status = complete_records ? lxp_log_recover_complete_records(&log, NULL, NULL) :
                                lxp_log_recover(&log, NULL, NULL);
    if (status != expected) {
        if (!close_descriptor) (void)lxp_log_close(&log);
        return 1;
    }
    if (close_descriptor) return 0;
    return lxp_log_close(&log) == LXP_OK ? 0 : 1;
}

static int durable_marker_retained(const char *path, uint64_t expected_end,
                                   uint64_t expected_next)
{
    lxp_log log;
    int retained;
    if (lxp_log_open(&log, path) != LXP_OK) return 1;
    retained = log.has_durable_marker && log.durable_offset == expected_end &&
               log.durable_next_sequence == expected_next;
    return lxp_log_close(&log) == LXP_OK && retained ? 0 : 1;
}

static int checkpointed_canonical_restart(void)
{
    static const uint64_t ends[] = {
        2U * LXP_RESTART_RECORD_BYTES, 3U * LXP_RESTART_RECORD_BYTES,
        5U * LXP_RESTART_RECORD_BYTES, 5U * LXP_RESTART_RECORD_BYTES
    };
    static const uint64_t nexts[] = { 22U, 22U, 23U, 23U };
    static const uint64_t heads[] = { 21U, 21U, 22U, 22U };
    static const uint32_t replays[] = { 2U, 1U, 3U, 3U };
    static const uint8_t kinds[] = {
        (uint8_t)LXP_LOG_ACTIVITY, (uint8_t)LXP_LOG_CHECKPOINT,
        (uint8_t)LXP_LOG_CHECKPOINT, (uint8_t)LXP_LOG_CHECKPOINT
    };
    uint32_t boundary;
    for (boundary = 1U; boundary <= 4U; ++boundary) {
        const uint32_t i = boundary - 1U;
        const uint64_t appended = ends[i] + 2U * LXP_RESTART_RECORD_BYTES;
        char directory[] = "/tmp/lxp-durable-canonical-XXXXXX";
        char path[128];
        int complete_records;
        int descriptor;
        uint8_t corrupt = 0xffU;
        if (mkdtemp(directory) == NULL ||
            crash_child(directory, boundary, 0, 90 + (int)boundary) != 0 ||
            snprintf(path, sizeof(path), "%s/%020u.lxp", directory, 0U) < 0)
            return 1;
        for (complete_records = 0; complete_records <= 1; ++complete_records)
            if (recover_restart(path, complete_records, ends[i], nexts[i],
                                heads[i], replays[i], kinds[i]) != 0)
                return 1;
        if (append_after_restart(path, (int)(boundary & 1U), 0, ends[i],
                                 nexts[i]) != 0)
            return 1;
        for (complete_records = 0; complete_records <= 1; ++complete_records)
            if (recover_restart(path, complete_records, appended,
                                nexts[i] + 1U, nexts[i], replays[i] + 2U,
                                kinds[i]) != 0)
                return 1;
        for (complete_records = 0; complete_records <= 1; ++complete_records)
            if (refuse_restart(path, complete_records, LXP_ERR_IO, 1) != 0 ||
                durable_marker_retained(path, appended, nexts[i] + 1U) != 0)
                return 1;
        descriptor = open(path, O_WRONLY | O_CLOEXEC);
        if (descriptor < 0 ||
            pwrite(descriptor, &corrupt, 1U,
                   (off_t)(LXP_RESTART_RECORD_BYTES +
                           LXP_LOG_HEADER_BYTES)) != 1 ||
            fdatasync(descriptor) != 0 || close(descriptor) != 0)
            return 1;
        for (complete_records = 0; complete_records <= 1; ++complete_records)
            if (refuse_restart(path, complete_records, LXP_ERR_LOG_CORRUPT,
                               0) != 0 ||
                durable_marker_retained(path, appended, nexts[i] + 1U) != 0)
                return 1;
        if (unlink(path) != 0 || rmdir(directory) != 0) return 1;
    }
    return 0;
}

static int existing_batch_restart(void)
{
    static const uint64_t ends[] = {
        2U * LXP_RESTART_RECORD_BYTES, 4U * LXP_RESTART_RECORD_BYTES,
        4U * LXP_RESTART_RECORD_BYTES
    };
    static const uint64_t nexts[] = { 6U, 7U, 7U };
    static const uint32_t replays[] = { 2U, 4U, 4U };
    uint32_t boundary;
    for (boundary = 1U; boundary <= 3U; ++boundary) {
        const uint32_t i = boundary - 1U;
        const uint64_t appended = ends[i] + LXP_RESTART_RECORD_BYTES;
        char directory[] = "/tmp/lxp-durable-batch-XXXXXX";
        char path[128];
        int descriptor;
        uint8_t corrupt = 0xffU;
        if (mkdtemp(directory) == NULL ||
            crash_child(directory, boundary, 1, 70 + (int)boundary) != 0 ||
            snprintf(path, sizeof(path), "%s/%020u.lxp", directory, 0U) < 0)
            return 1;
        if (recover_restart(path, 1, ends[i], nexts[i], UINT64_MAX,
                            replays[i], (uint8_t)LXP_LOG_BATCH_HEADER) != 0 ||
            refuse_restart(path, 0, LXP_ERR_LOG_CORRUPT, 0) != 0 ||
            durable_marker_retained(path, ends[i], nexts[i]) != 0 ||
            recover_restart(path, 1, ends[i], nexts[i], UINT64_MAX,
                            replays[i], (uint8_t)LXP_LOG_BATCH_HEADER) != 0 ||
            append_after_restart(path, 1, 1, ends[i], nexts[i]) != 0 ||
            recover_restart(path, 1, appended, nexts[i] + 1U, UINT64_MAX,
                            replays[i] + 1U,
                            (uint8_t)LXP_LOG_BATCH_HEADER) != 0 ||
            refuse_restart(path, 1, LXP_ERR_IO, 1) != 0 ||
            durable_marker_retained(path, appended, nexts[i] + 1U) != 0)
            return 1;
        descriptor = open(path, O_WRONLY | O_CLOEXEC);
        if (descriptor < 0 ||
            pwrite(descriptor, &corrupt, 1U,
                   (off_t)(LXP_RESTART_RECORD_BYTES +
                           LXP_LOG_HEADER_BYTES)) != 1 ||
            fdatasync(descriptor) != 0 || close(descriptor) != 0)
            return 1;
        if (refuse_restart(path, 1, LXP_ERR_LOG_CORRUPT, 0) != 0 ||
            durable_marker_retained(path, appended, nexts[i] + 1U) != 0 ||
            unlink(path) != 0 || rmdir(directory) != 0)
            return 1;
    }
    return 0;
}

int main(void)
{
    uint32_t boundary;
    for (boundary = 1U; boundary <= 3U; ++boundary) {
        char directory[] = "/tmp/lxp-durable-XXXXXX";
        char path[128];
        pid_t child;
        int child_status;
        lxp_log log;
        uint64_t durable;
        if (mkdtemp(directory) == NULL) return 1;
        child = fork();
        if (child < 0) return 1;
        if (child == 0) _exit(run_child(directory, boundary));
        if (waitpid(child, &child_status, 0) != child ||
            !WIFEXITED(child_status) || WEXITSTATUS(child_status) == 0)
            return 1;
        if (snprintf(path, sizeof(path), "%s/%020u.lxp", directory, 0U) < 0)
            return 1;
        if (lxp_log_open(&log, path) != LXP_OK ||
            lxp_log_durable_head(&log, &durable) != LXP_OK) return 1;
        if ((boundary == 1U && durable != UINT64_MAX) ||
            (boundary > 1U && durable != 11U)) return 1;
        if (lxp_log_close(&log) != LXP_OK || unlink(path) != 0 ||
            rmdir(directory) != 0) return 1;
    }
    {
        char directory[] = "/tmp/lxp-durable-group-XXXXXX";
        char first_path[128];
        char second_path[128];
        const uint8_t first[] = { 1U, 2U };
        const uint8_t second[] = { 3U, 4U };
        const uint8_t later[] = { 5U, 6U };
        lxp_durability_group group;
        lxp_log first_log;
        lxp_log second_log;
        uint64_t durable;
        int descriptor;
        int directory_descriptor;
        uint8_t corrupt = 0xffU;
        off_t first_pair = (off_t)(2U *
            (LXP_LOG_HEADER_BYTES + sizeof(first)));
        off_t later_body = first_pair + (off_t)LXP_LOG_HEADER_BYTES;
        if (mkdtemp(directory) == NULL ||
            lxp_log_segment_create(&first_log, directory, 0U, 4096U) != LXP_OK ||
            lxp_log_segment_create(&second_log, directory, 1U, 4096U) != LXP_OK ||
            lxp_durability_group_begin(&group) != LXP_OK ||
            lxp_log_append(&first_log, LXP_LOG_ACTIVITY, 11U, first,
                           (uint32_t)sizeof(first), NULL) != LXP_OK ||
            lxp_log_append(&first_log, LXP_LOG_RECEIPT, 11U, first,
                           (uint32_t)sizeof(first), NULL) != LXP_OK ||
            lxp_log_write_boundary(&first_log) != LXP_OK ||
            lxp_log_append(&second_log, LXP_LOG_ACTIVITY, 12U, second,
                           (uint32_t)sizeof(second), NULL) != LXP_OK ||
            lxp_log_append(&second_log, LXP_LOG_RECEIPT, 12U, second,
                           (uint32_t)sizeof(second), NULL) != LXP_OK ||
            lxp_log_write_boundary(&second_log) != LXP_OK)
            return 1;
        directory_descriptor = open(directory, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
        if (directory_descriptor < 0 ||
            !lxp_durability_group_defer_descriptor(directory_descriptor) ||
            close(directory_descriptor) != 0 ||
            lxp_durability_group_commit(&group) != LXP_OK)
            return 1;
        if (lxp_log_durable_head(&first_log, &durable) != LXP_OK ||
            durable != 11U)
            return 1;
        if (lxp_log_durable_head(&second_log, &durable) != LXP_OK ||
            durable != 12U)
            return 1;
        if (
            lxp_durability_group_begin(&group) != LXP_OK ||
            lxp_log_append(&first_log, LXP_LOG_ACTIVITY, 13U, later,
                           (uint32_t)sizeof(later), NULL) != LXP_OK ||
            lxp_log_append(&first_log, LXP_LOG_RECEIPT, 13U, later,
                           (uint32_t)sizeof(later), NULL) != LXP_OK ||
            lxp_log_write_boundary(&first_log) != LXP_OK ||
            lxp_log_append(&second_log, LXP_LOG_ACTIVITY, 14U, later,
                           (uint32_t)sizeof(later), NULL) != LXP_OK ||
            lxp_log_append(&second_log, LXP_LOG_RECEIPT, 14U, later,
                           (uint32_t)sizeof(later), NULL) != LXP_OK ||
            lxp_log_write_boundary(&second_log) != LXP_OK ||
            lxp_durability_group_commit(&group) != LXP_OK ||
            snprintf(first_path, sizeof(first_path), "%s/%020u.lxp",
                     directory, 0U) < 0 ||
            snprintf(second_path, sizeof(second_path), "%s/%020u.lxp",
                     directory, 1U) < 0 ||
            lxp_log_close(&first_log) != LXP_OK ||
            lxp_log_close(&second_log) != LXP_OK)
            return 1;
        descriptor = open(first_path, O_WRONLY | O_CLOEXEC);
        if (descriptor < 0 || pwrite(descriptor, &corrupt, 1U, later_body) != 1 ||
            fdatasync(descriptor) != 0 || close(descriptor) != 0)
            return 1;
        lxp_log_set_prepared_recovery(true);
        if (
            lxp_log_open(&first_log, first_path) != LXP_OK ||
            lxp_log_recover(&first_log, NULL, NULL) != LXP_OK ||
            lxp_log_durable_head(&first_log, &durable) != LXP_OK ||
            durable != 11U ||
            first_log.write_offset != (uint64_t)first_pair ||
            lxp_log_close(&first_log) != LXP_OK ||
            unlink(first_path) != 0 || unlink(second_path) != 0 ||
            rmdir(directory) != 0)
            return 1;
        lxp_log_set_prepared_recovery(false);
    }
    {
        char directory[] = "/tmp/lxp-durable-failure-XXXXXX";
        char path[128];
        const uint8_t bytes[] = {1U, 2U};
        lxp_log log;
        lxp_durability_group group;
        uint64_t durable;
        if (mkdtemp(directory) == NULL ||
            lxp_log_segment_create(&log, directory, 0U, 4096U) != LXP_OK ||
            lxp_durability_group_begin(&group) != LXP_OK ||
            lxp_log_append(&log, LXP_LOG_ACTIVITY, 1U, bytes, sizeof(bytes), NULL) != LXP_OK ||
            lxp_log_append(&log, LXP_LOG_RECEIPT, 1U, bytes, sizeof(bytes), NULL) != LXP_OK ||
            lxp_log_write_boundary(&log) != LXP_OK ||
            group.descriptor_count != 1U || close(group.descriptors[0]) != 0 ||
            lxp_durability_group_commit(&group) != LXP_ERR_IO || group.active ||
            log.durable_offset != 0U || log.durable_generation != 0U ||
            lxp_log_durable_head(&log, &durable) != LXP_OK || durable != UINT64_MAX ||
            lxp_log_close(&log) != LXP_OK ||
            snprintf(path, sizeof(path), "%s/%020u.lxp", directory, 0U) < 0 ||
            unlink(path) != 0 || rmdir(directory) != 0)
            return 1;
    }
    if (checkpointed_canonical_restart() != 0 ||
        existing_batch_restart() != 0)
        return 1;
    return 0;
}
