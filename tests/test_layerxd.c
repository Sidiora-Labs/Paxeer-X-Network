#define _POSIX_C_SOURCE 200809L

#include "layerx/lxp_daemon.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_paxeer.h"
#include "lxp_daemon_finality_authority.h"

#include <arpa/inet.h>
#include <netinet/in.h>
#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

typedef struct apply_state {
    uint64_t expected_sequence;
    uint8_t root[32];
} apply_state;

#define REQUIRE(condition, label) do { \
    if (!(condition)) { \
        (void)fprintf(stderr, "test_layerxd: %s\n", (label)); \
        return 1; \
    } \
} while (0)

static lxp_result apply_activity(
    void *context, uint64_t global_sequence,
    const uint8_t *activity, size_t activity_length)
{
    apply_state *state = (apply_state *)context;
    uint8_t preimage[32U + 8U + LXP_MAX_ACTIVITY_BYTES];
    size_t i;
    if (state == NULL || activity == NULL ||
        global_sequence != state->expected_sequence)
        return LXP_ERR_SEQUENCE_GAP;
    (void)memcpy(preimage, state->root, 32U);
    for (i = 0U; i < 8U; ++i)
        preimage[39U - i] = (uint8_t)(global_sequence >> (i * 8U));
    (void)memcpy(preimage + 40U, activity, activity_length);
    if (lxp_hash_domain(
            LXP_DOMAIN_STATE_LEAF, preimage,
            40U + activity_length, state->root) != LXP_OK)
        return LXP_ERR_BAD_SIGNATURE;
    ++state->expected_sequence;
    return LXP_OK;
}

static int write_text(char path[64], const char *text)
{
    int descriptor = mkstemp(path);
    FILE *file;
    int result;
    if (descriptor < 0) return 1;
    file = fdopen(descriptor, "wb");
    if (file == NULL) {
        (void)close(descriptor);
        return 1;
    }
    result = fputs(text, file);
    return result < 0 || fclose(file) != 0;
}

static int write_config(
    char path[64], const char *role, uint64_t start_sequence,
    size_t workers, bool serial_execution)
{
    char text[512];
    int written = snprintf(
        text, sizeof(text),
        "config_version=2\n"
        "role=%s\n"
        "network_id=42\n"
        "start_sequence=%llu\n"
        "verify_workers=%zu\n"
        "serial_execution=%s\n",
        role, (unsigned long long)start_sequence, workers,
        serial_execution ? "true" : "false");
    if (written < 0 || (size_t)written >= sizeof(text)) return 1;
    return write_text(path, text);
}

static int write_negative_sequence_config(char path[64])
{
    return write_text(
        path,
        "config_version=2\n"
        "role=sequencer\n"
        "network_id=42\n"
        "start_sequence=-1\n"
        "verify_workers=0\n"
        "serial_execution=true\n");
}

static int write_legacy_config(
    char path[64], size_t verify, size_t network, size_t projection,
    size_t checkpoint, bool serial_execution)
{
    char text[512];
    int written = snprintf(
        text, sizeof(text),
        "role=sequencer\n"
        "network_id=42\n"
        "start_sequence=0\n"
        "verify_workers=%zu\n"
        "network_workers=%zu\n"
        "projection_workers=%zu\n"
        "checkpoint_workers=%zu\n"
        "serial_execution=%s\n",
        verify, network, projection, checkpoint,
        serial_execution ? "true" : "false");
    if (written < 0 || (size_t)written >= sizeof(text)) return 1;
    return write_text(path, text);
}

typedef struct config_case {
    const char *name;
    const char *text;
    lxp_result expected;
    uint32_t version;
    size_t verify_workers;
    bool serial_execution;
} config_case;

static const config_case CONFIG_CASES[] = {
    {"v2 parallel",
     "config_version=2\nrole=sequencer\nnetwork_id=42\nstart_sequence=0\n"
     "verify_workers=2\nserial_execution=false\n",
     LXP_OK, LXP_DAEMON_CONFIG_VERSION, 2U, false},
    {"v2 zero workers",
     "config_version=2\nrole=replica\nnetwork_id=42\nstart_sequence=7\n"
     "verify_workers=0\nserial_execution=false\n",
     LXP_OK, LXP_DAEMON_CONFIG_VERSION, 0U, false},
    {"v2 maximum workers",
     "config_version=2\nrole=guarantor\nnetwork_id=42\nstart_sequence=0\n"
     "verify_workers=16\nserial_execution=false\n",
     LXP_OK, LXP_DAEMON_CONFIG_VERSION, 16U, false},
    {"v2 serial",
     "config_version=2\nrole=sequencer\nnetwork_id=42\nstart_sequence=0\n"
     "verify_workers=0\nserial_execution=true\n",
     LXP_OK, LXP_DAEMON_CONFIG_VERSION, 0U, true},
    {"v2 over limit",
     "config_version=2\nrole=sequencer\nnetwork_id=42\nstart_sequence=0\n"
     "verify_workers=17\nserial_execution=false\n",
     LXP_ERR_LENGTH_LIMIT, 0U, 0U, false},
    {"v2 serial with workers",
     "config_version=2\nrole=sequencer\nnetwork_id=42\nstart_sequence=0\n"
     "verify_workers=1\nserial_execution=true\n",
     LXP_ERR_NON_CANONICAL, 0U, 0U, false},
    {"v2 network pool key",
     "config_version=2\nrole=sequencer\nnetwork_id=42\nstart_sequence=0\n"
     "verify_workers=2\nnetwork_workers=0\nserial_execution=false\n",
     LXP_ERR_UNKNOWN_FIELD, 0U, 0U, false},
    {"v2 projection pool key",
     "config_version=2\nrole=sequencer\nnetwork_id=42\nstart_sequence=0\n"
     "verify_workers=2\nprojection_workers=2\nserial_execution=false\n",
     LXP_ERR_UNKNOWN_FIELD, 0U, 0U, false},
    {"v2 checkpoint pool key",
     "config_version=2\nrole=sequencer\nnetwork_id=42\nstart_sequence=0\n"
     "verify_workers=2\nserial_execution=false\ncheckpoint_workers=1\n",
     LXP_ERR_UNKNOWN_FIELD, 0U, 0U, false},
    {"v2 unknown key",
     "config_version=2\nrole=sequencer\nnetwork_id=42\nstart_sequence=0\n"
     "verify_workers=2\nserial_execution=false\nexecutor_threads=4\n",
     LXP_ERR_UNKNOWN_FIELD, 0U, 0U, false},
    {"unsupported version",
     "config_version=3\nrole=sequencer\nnetwork_id=42\nstart_sequence=0\n"
     "verify_workers=2\nserial_execution=false\n",
     LXP_ERR_VERSION_UNSUPPORTED, 0U, 0U, false},
    {"legacy zero pools deprecated",
     "role=sequencer\nnetwork_id=42\nstart_sequence=0\nverify_workers=4\n"
     "network_workers=0\nprojection_workers=0\ncheckpoint_workers=0\n"
     "serial_execution=false\n",
     LXP_OK, LXP_DAEMON_CONFIG_VERSION_LEGACY, 4U, false},
    {"legacy serial deprecated",
     "role=sequencer\nnetwork_id=42\nstart_sequence=0\nverify_workers=0\n"
     "network_workers=0\nprojection_workers=0\ncheckpoint_workers=0\n"
     "serial_execution=true\n",
     LXP_OK, LXP_DAEMON_CONFIG_VERSION_LEGACY, 0U, true},
    {"legacy network pool",
     "role=sequencer\nnetwork_id=42\nstart_sequence=0\nverify_workers=2\n"
     "network_workers=2\nprojection_workers=0\ncheckpoint_workers=0\n"
     "serial_execution=false\n",
     LXP_ERR_VERSION_UNSUPPORTED, 0U, 0U, false},
    {"legacy projection pool",
     "role=sequencer\nnetwork_id=42\nstart_sequence=0\nverify_workers=2\n"
     "network_workers=0\nprojection_workers=2\ncheckpoint_workers=0\n"
     "serial_execution=false\n",
     LXP_ERR_VERSION_UNSUPPORTED, 0U, 0U, false},
    {"legacy checkpoint pool",
     "role=sequencer\nnetwork_id=42\nstart_sequence=0\nverify_workers=2\n"
     "network_workers=0\nprojection_workers=0\ncheckpoint_workers=1\n"
     "serial_execution=false\n",
     LXP_ERR_VERSION_UNSUPPORTED, 0U, 0U, false},
    {"legacy pool over old bound",
     "role=sequencer\nnetwork_id=42\nstart_sequence=0\nverify_workers=2\n"
     "network_workers=17\nprojection_workers=0\ncheckpoint_workers=0\n"
     "serial_execution=false\n",
     LXP_ERR_LENGTH_LIMIT, 0U, 0U, false},
    {"legacy verify over limit",
     "role=sequencer\nnetwork_id=42\nstart_sequence=0\nverify_workers=17\n"
     "network_workers=0\nprojection_workers=0\ncheckpoint_workers=0\n"
     "serial_execution=false\n",
     LXP_ERR_LENGTH_LIMIT, 0U, 0U, false},
    {"legacy serial with workers",
     "role=sequencer\nnetwork_id=42\nstart_sequence=0\nverify_workers=3\n"
     "network_workers=0\nprojection_workers=0\ncheckpoint_workers=0\n"
     "serial_execution=true\n",
     LXP_ERR_NON_CANONICAL, 0U, 0U, false}};

static int config_cases(void)
{
    size_t i;
    for (i = 0U; i < sizeof(CONFIG_CASES) / sizeof(CONFIG_CASES[0]); ++i) {
        const config_case *item = &CONFIG_CASES[i];
        char path[64] = "/tmp/layerxd-case-XXXXXX";
        lxp_daemon_configuration config;
        lxp_result status;
        if (write_text(path, item->text) != 0) return 1;
        (void)memset(&config, 0, sizeof(config));
        status = lxp_daemon_config_load(path, &config);
        if (unlink(path) != 0) return 1;
        if (status != item->expected) {
            (void)fprintf(
                stderr, "test_layerxd: config case %s: %d expected %d\n",
                item->name, status, item->expected);
            return 1;
        }
        if (status == LXP_OK &&
            (config.config_version != item->version ||
             config.verify_workers != item->verify_workers ||
             config.serial_execution != item->serial_execution ||
             config.network_id != 42U)) {
            (void)fprintf(
                stderr, "test_layerxd: config case %s: fields\n", item->name);
            return 1;
        }
    }
    return 0;
}

static int effective_worker_cases(void)
{
    lxp_daemon_configuration config;
    uint32_t workers = 0U;
    char line[256];
    char small[16];
    static const char expected_parallel[] =
        "concurrency config_version=2 verify_workers=4 "
        "effective_program_workers=4 serial_execution=false "
        "owner=programs-kernel-prepare daemon_threads=executor";
    static const char expected_serial[] =
        "concurrency config_version=2 verify_workers=0 "
        "effective_program_workers=1 serial_execution=true "
        "owner=programs-kernel-prepare daemon_threads=executor";
    (void)memset(&config, 0, sizeof(config));
    config.role = LXP_DAEMON_SEQUENCER;
    config.network_id = 42U;
    config.config_version = LXP_DAEMON_CONFIG_VERSION;
    if (lxp_daemon_effective_verify_workers(NULL, &workers) !=
        LXP_ERR_NON_CANONICAL)
        return 1;
    config.verify_workers = 0U;
    if (lxp_daemon_effective_verify_workers(&config, &workers) != LXP_OK ||
        workers != 1U)
        return 1;
    config.verify_workers = LXP_DAEMON_MAX_VERIFY_WORKERS;
    if (lxp_daemon_effective_verify_workers(&config, &workers) != LXP_OK ||
        workers != LXP_DAEMON_MAX_VERIFY_WORKERS)
        return 1;
    config.verify_workers = LXP_DAEMON_MAX_VERIFY_WORKERS + 1U;
    if (lxp_daemon_effective_verify_workers(&config, &workers) !=
        LXP_ERR_LENGTH_LIMIT)
        return 1;
    config.verify_workers = 2U;
    config.serial_execution = true;
    if (lxp_daemon_effective_verify_workers(&config, &workers) !=
        LXP_ERR_NON_CANONICAL)
        return 1;
    config.verify_workers = 0U;
    if (lxp_daemon_effective_verify_workers(&config, &workers) != LXP_OK ||
        workers != 1U)
        return 1;
    if (lxp_daemon_concurrency_report(&config, line, sizeof(line)) !=
            LXP_OK ||
        strcmp(line, expected_serial) != 0) {
        (void)fprintf(stderr, "test_layerxd: report serial: %s\n", line);
        return 1;
    }
    config.serial_execution = false;
    config.verify_workers = 4U;
    if (lxp_daemon_concurrency_report(&config, line, sizeof(line)) !=
            LXP_OK ||
        strcmp(line, expected_parallel) != 0) {
        (void)fprintf(stderr, "test_layerxd: report parallel: %s\n", line);
        return 1;
    }
    if (lxp_daemon_concurrency_report(&config, small, sizeof(small)) !=
        LXP_ERR_LENGTH_LIMIT)
        return 1;
    return 0;
}

static int submit_range(
    lxp_daemon *daemon, uint64_t first, uint64_t count)
{
    uint64_t i;
    for (i = 0U; i < count; ++i) {
        uint8_t activity[24];
        lxp_result status;
        size_t retry;
        size_t j;
        for (j = 0U; j < sizeof(activity); ++j)
            activity[j] = (uint8_t)((first + i + j) & UINT64_C(0xff));
        status = LXP_ERR_LENGTH_LIMIT;
        for (retry = 0U; retry < 10000U &&
             status == LXP_ERR_LENGTH_LIMIT; ++retry) {
            struct timespec interval = {0, 1000000L};
            status = lxp_daemon_submit(daemon, activity, sizeof(activity));
            if (status == LXP_ERR_LENGTH_LIMIT &&
                nanosleep(&interval, NULL) != 0)
                return 1;
        }
        if (status != LXP_OK) {
            (void)fprintf(
                stderr, "test_layerxd: submit %llu: %d\n",
                (unsigned long long)(first + i), status);
            return 1;
        }
    }
    return 0;
}

static int await_sequence(lxp_daemon *daemon, uint64_t expected)
{
    size_t retry;
    uint64_t observed = 0U;
    lxp_result failure = LXP_OK;
    for (retry = 0U; retry < 30000U; ++retry) {
        struct timespec interval = {0, 1000000L};
        (void)pthread_mutex_lock(&daemon->mutex);
        observed = daemon->next_sequence;
        failure = daemon->failure;
        (void)pthread_mutex_unlock(&daemon->mutex);
        if (observed == expected) return 0;
        if (failure != LXP_OK || observed > expected) break;
        if (nanosleep(&interval, NULL) != 0) break;
    }
    (void)fprintf(
        stderr,
        "test_layerxd: await sequence expected=%llu observed=%llu "
        "failure=%d\n",
        (unsigned long long)expected, (unsigned long long)observed,
        failure);
    return 1;
}

static int run_window(
    const lxp_daemon_configuration *config,
    uint64_t count, uint8_t root[32], bool await_all)
{
    static lxp_daemon daemon;
    apply_state state;
    lxp_result status;
    (void)memset(&state, 0, sizeof(state));
    state.expected_sequence = config->start_sequence;
    status = lxp_daemon_start(&daemon, config, apply_activity, &state);
    if (status != LXP_OK) {
        (void)fprintf(stderr, "test_layerxd: window start: %d\n", status);
        return 1;
    }
    if (submit_range(&daemon, config->start_sequence, count) != 0) {
        (void)fprintf(stderr, "test_layerxd: window submit\n");
        return 1;
    }
    if (await_all && await_sequence(&daemon, config->start_sequence + count) != 0)
        return 1;
    status = lxp_daemon_shutdown(&daemon);
    if (status != LXP_OK) {
        (void)fprintf(stderr, "test_layerxd: window shutdown: %d\n", status);
        return 1;
    }
    if (state.expected_sequence != config->start_sequence + count ||
        daemon.next_sequence != state.expected_sequence ||
        daemon.executed_count != count) {
        (void)fprintf(stderr, "test_layerxd: window sequence\n");
        return 1;
    }
    (void)memcpy(root, state.root, 32U);
    return 0;
}

static int start_refusals(void)
{
    static lxp_daemon daemon;
    lxp_daemon_configuration config;
    apply_state state;
    (void)memset(&config, 0, sizeof(config));
    (void)memset(&state, 0, sizeof(state));
    config.role = LXP_DAEMON_SEQUENCER;
    config.network_id = 42U;
    config.config_version = LXP_DAEMON_CONFIG_VERSION;
    config.verify_workers = LXP_DAEMON_MAX_VERIFY_WORKERS + 1U;
    if (lxp_daemon_start(&daemon, &config, apply_activity, &state) !=
            LXP_ERR_LENGTH_LIMIT ||
        daemon.primitives_initialized)
        return 1;
    config.verify_workers = 1U;
    config.serial_execution = true;
    if (lxp_daemon_start(&daemon, &config, apply_activity, &state) !=
            LXP_ERR_NON_CANONICAL ||
        daemon.primitives_initialized)
        return 1;
    config.verify_workers = 0U;
    config.network_id = 0U;
    if (lxp_daemon_start(&daemon, &config, apply_activity, &state) !=
            LXP_ERR_NON_CANONICAL ||
        daemon.primitives_initialized)
        return 1;
    config.network_id = 42U;
    if (lxp_daemon_start(&daemon, &config, apply_activity, &state) !=
            LXP_OK ||
        submit_range(&daemon, 0U, 64U) != 0 ||
        await_sequence(&daemon, 64U) != 0 ||
        lxp_daemon_shutdown(&daemon) != LXP_OK ||
        daemon.executed_count != 64U || state.expected_sequence != 64U)
        return 1;
    return lxp_daemon_shutdown(&daemon) != LXP_ERR_NON_CANONICAL;
}

static int startup_thread_failure_child(int resource)
{
    static lxp_daemon daemon;
    lxp_daemon_configuration config;
    apply_state state;
    struct rlimit original, limit;
    lxp_result status;
    if (geteuid() == 0 && (setgid(65534) != 0 || setuid(65534) != 0))
        return 3;
    if (getrlimit(resource, &original) != 0) return 4;
    limit = original;
    limit.rlim_cur = 0;
    if (setrlimit(resource, &limit) != 0) return 4;
    (void)memset(&config, 0, sizeof(config));
    (void)memset(&state, 0, sizeof(state));
    config.role = LXP_DAEMON_SEQUENCER;
    config.network_id = 42U;
    config.config_version = LXP_DAEMON_CONFIG_VERSION;
    config.verify_workers = 4U;
    status = lxp_daemon_start(&daemon, &config, apply_activity, &state);
    if (status != LXP_ERR_IO) return 5;
    if (daemon.primitives_initialized || daemon.executor_started ||
        daemon.executed_count != 0U)
        return 6;
    if (lxp_daemon_shutdown(&daemon) != LXP_ERR_NON_CANONICAL) return 7;
    if (setrlimit(resource, &original) != 0) return 8;
    if (lxp_daemon_start(&daemon, &config, apply_activity, &state) != LXP_OK)
        return 9;
    if (lxp_daemon_shutdown(&daemon) != LXP_OK ||
        daemon.executor_started || daemon.primitives_initialized)
        return 10;
    return 0;
}

static int startup_thread_failure(int resource)
{
    pid_t child = fork();
    int wait_status = 0;
    if (child < 0) return 1;
    if (child == 0) {
        (void)execl("/proc/self/exe", "test_layerxd", "--startup-failure",
                    resource == RLIMIT_AS ? "allocation" : "thread", (char *)NULL);
        _exit(11);
    }
    while (waitpid(child, &wait_status, 0) < 0)
        if (errno != EINTR) return 1;
    if (!WIFEXITED(wait_status) || WEXITSTATUS(wait_status) != 0) {
        (void)fprintf(stderr,
                      "test_layerxd: startup resource %d child status %d\n",
                      resource, wait_status);
        return 1;
    }
    return 0;
}

enum { HTTP_CAPACITY = 262143, HTTP_BUFFER = 262144 };

typedef struct http_case {
    const char *name;
    const char *text;
    lxp_daemon_http_parse expected;
    const char *body;
} http_case;

static const char HTTP_CHUNKED_OK[] =
    "HTTP/1.1 200 OK\r\n"
    "Content-Type: application/json\r\n"
    "Transfer-Encoding: chunked\r\n"
    "\r\n"
    "9\r\n{\"result\"\r\n"
    "6\r\n:true}\r\n"
    "0\r\n\r\n";

static const http_case HTTP_CASES[] = {
    {"identity body",
     "HTTP/1.1 200 OK\r\nContent-Length: 15\r\n\r\n{\"result\":true}",
     LXP_DAEMON_HTTP_COMPLETE, "{\"result\":true}"},
    {"identity short",
     "HTTP/1.1 200 OK\r\nContent-Length: 15\r\n\r\n{\"result\":tru",
     LXP_DAEMON_HTTP_INCOMPLETE, NULL},
    {"identity overrun",
     "HTTP/1.1 200 OK\r\nContent-Length: 14\r\n\r\n{\"result\":true}",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"chunked body", HTTP_CHUNKED_OK,
     LXP_DAEMON_HTTP_COMPLETE, "{\"result\":true}"},
    {"chunked mixed case and trailer",
     "HTTP/1.1 200 OK\r\ntransfer-encoding:  CHUNKED \r\n\r\n"
     "A\r\n0123456789\r\n6\r\nabcdef\r\n0\r\nX-Checksum: 1\r\n\r\n",
     LXP_DAEMON_HTTP_COMPLETE, "0123456789abcdef"},
    {"http/1.0 chunked",
     "HTTP/1.0 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_COMPLETE, "okay"},
    {"chunked truncated terminator",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nokay\r\n0\r\n",
     LXP_DAEMON_HTTP_INCOMPLETE, NULL},
    {"chunked truncated data",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nok",
     LXP_DAEMON_HTTP_INCOMPLETE, NULL},
    {"chunk extension rejected",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n"
     "4;name=value\r\nokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"chunk size not hex",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\nzz\r\nokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"chunk size missing",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n\r\nokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"chunk size digits unbounded",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n"
     "00000000000000004\r\nokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"chunk size above capacity",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\nffffffff\r\nokay\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"chunk data unterminated",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nokayXX0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"chunk line lone carriage return",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\rokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"chunked body empty",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"chunked trailing bytes",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nokay\r\n0\r\n\r\nx",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"chunked trailer without colon",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n"
     "4\r\nokay\r\n0\r\nbogus\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"length and chunked together",
     "HTTP/1.1 200 OK\r\nContent-Length: 4\r\nTransfer-Encoding: chunked\r\n\r\n"
     "4\r\nokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"transfer encoding not chunked",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: gzip\r\n\r\n4\r\nokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"transfer encoding repeated",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n"
     "Transfer-Encoding: chunked\r\n\r\n4\r\nokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"transfer encoding with trailing token",
     "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked, gzip\r\n\r\n"
     "4\r\nokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"framing absent", "HTTP/1.1 200 OK\r\nServer: paxd\r\n\r\n{}",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"status not ok",
     "HTTP/1.1 500 Internal Server Error\r\nTransfer-Encoding: chunked\r\n\r\n"
     "4\r\nokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"header without colon",
     "HTTP/1.1 200 OK\r\nbogus\r\nTransfer-Encoding: chunked\r\n\r\n"
     "4\r\nokay\r\n0\r\n\r\n",
     LXP_DAEMON_HTTP_MALFORMED, NULL},
    {"headers incomplete", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n",
     LXP_DAEMON_HTTP_INCOMPLETE, NULL}};

static int http_parse_cases(void)
{
    static char buffer[HTTP_BUFFER];
    size_t i;
    for (i = 0U; i < sizeof(HTTP_CASES) / sizeof(HTTP_CASES[0]); ++i) {
        const http_case *item = &HTTP_CASES[i];
        lxp_daemon_http_response message;
        size_t length = strlen(item->text);
        lxp_daemon_http_parse parsed;
        (void)memset(&message, 0, sizeof(message));
        (void)memcpy(buffer, item->text, length);
        parsed = lxp_daemon_http_response_parse(
            buffer, length, HTTP_CAPACITY, &message);
        if (parsed != item->expected) {
            (void)fprintf(
                stderr, "test_layerxd: http case %s: parsed %d expected %d\n",
                item->name, (int)parsed, (int)item->expected);
            return 1;
        }
        if (item->body == NULL) continue;
        if (message.body_length != strlen(item->body) ||
            memcmp(buffer + message.body_offset, item->body,
                   message.body_length) != 0) {
            (void)fprintf(
                stderr, "test_layerxd: http case %s: body mismatch\n",
                item->name);
            return 1;
        }
    }
    return 0;
}

static int http_oversize_header(void)
{
    static char buffer[HTTP_BUFFER];
    lxp_daemon_http_response message;
    size_t length = 8193U;
    (void)memset(&message, 0, sizeof(message));
    (void)memset(buffer, 'a', length);
    (void)memcpy(buffer, "HTTP/1.1 200 OK\r\n", 17U);
    return lxp_daemon_http_response_parse(
               buffer, length, HTTP_CAPACITY, &message) !=
           LXP_DAEMON_HTTP_MALFORMED;
}

static int http_incremental_chunked(void)
{
    static char buffer[HTTP_BUFFER];
    size_t length = sizeof(HTTP_CHUNKED_OK) - 1U;
    size_t prefix;
    for (prefix = 0U; prefix <= length; ++prefix) {
        lxp_daemon_http_response message;
        lxp_daemon_http_parse parsed;
        lxp_daemon_http_parse expected = prefix == length
            ? LXP_DAEMON_HTTP_COMPLETE : LXP_DAEMON_HTTP_INCOMPLETE;
        (void)memset(&message, 0, sizeof(message));
        (void)memcpy(buffer, HTTP_CHUNKED_OK, prefix);
        parsed = lxp_daemon_http_response_parse(
            buffer, prefix, HTTP_CAPACITY, &message);
        if (parsed != expected) {
            (void)fprintf(
                stderr,
                "test_layerxd: chunked prefix %zu: parsed %d expected %d\n",
                prefix, (int)parsed, (int)expected);
            return 1;
        }
        if (parsed == LXP_DAEMON_HTTP_COMPLETE &&
            (message.body_length != 15U ||
             memcmp(buffer + message.body_offset, "{\"result\":true}",
                    15U) != 0))
            return 1;
    }
    return 0;
}

typedef struct rpc_server {
    int listener;
    const char *response;
    size_t length;
} rpc_server;

static void *serve_response(void *context)
{
    rpc_server *server = (rpc_server *)context;
    char request[4096];
    int client = accept(server->listener, NULL, NULL);
    ssize_t count;
    if (client < 0) return NULL;
    count = recv(client, request, sizeof(request), 0);
    if (count > 0)
        (void)send(client, server->response, server->length, MSG_NOSIGNAL);
    (void)close(client);
    return NULL;
}

static void hex_address(const uint8_t *bytes, size_t length, char *text)
{
    static const char digits[] = "0123456789abcdef";
    size_t i;
    text[0] = '0';
    text[1] = 'x';
    for (i = 0U; i < length; ++i) {
        text[2U + i * 2U] = digits[bytes[i] >> 4U];
        text[3U + i * 2U] = digits[bytes[i] & 15U];
    }
    text[2U + length * 2U] = '\0';
}

static int finality_rpc_case(
    const char *response, size_t length, lxp_result expected,
    lxp_daemon_anchor_ladder expected_ladder)
{
    lxp_daemon_finality_authority authority;
    struct sockaddr_in address;
    socklen_t address_length = sizeof(address);
    rpc_server server;
    pthread_t thread;
    char anchor[43], port[16];
    lxp_daemon_anchor_ladder ladder = LXP_DAEMON_ANCHOR_INSTANT;
    lxp_result status;
    int listener = socket(AF_INET, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (listener < 0) return 1;
    (void)memset(&address, 0, sizeof(address));
    address.sin_family = AF_INET;
    address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    if (bind(listener, (const struct sockaddr *)&address, sizeof(address)) !=
            0 ||
        listen(listener, 1) != 0 ||
        getsockname(listener, (struct sockaddr *)&address, &address_length) !=
            0) {
        (void)close(listener);
        return 1;
    }
    hex_address(lxp_paxeer_anchor_address, 20U, anchor);
    (void)snprintf(
        port, sizeof(port), "%u", (unsigned)ntohs(address.sin_port));
    if (setenv("LAYERX_NODE_PAXEER_RPC_ADDRESS", "127.0.0.1", 1) != 0 ||
        setenv("LAYERX_NODE_PAXEER_CHAIN_ID", "9125", 1) != 0 ||
        setenv("LAYERX_NODE_PAXEER_RPC_PORT", port, 1) != 0 ||
        setenv("LAYERX_NODE_SETTLEMENT_CONTRACT", anchor, 1) != 0 ||
        setenv("LAYERX_NODE_CHECKPOINT_REGISTRY", anchor, 1) != 0 ||
        lxp_daemon_finality_authority_init_pins(&authority) != LXP_OK) {
        (void)close(listener);
        return 1;
    }
    server.listener = listener;
    server.response = response;
    server.length = length;
    if (pthread_create(&thread, NULL, serve_response, &server) != 0) {
        (void)close(listener);
        return 1;
    }
    status = lxp_daemon_finality_authority_ladder(&authority, 1U, &ladder);
    (void)pthread_join(thread, NULL);
    (void)close(listener);
    if (status != expected) {
        (void)fprintf(
            stderr, "test_layerxd: finality rpc status %d expected %d\n",
            status, expected);
        return 1;
    }
    return status == LXP_OK && ladder != expected_ladder;
}

static int finality_rpc_chunked(void)
{
    static const char body[] =
        "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"0x"
        "0000000000000000000000000000000000000000000000000000000000000002\"}";
    char response[512];
    size_t length = sizeof(body) - 1U;
    size_t split = 40U;
    int written = snprintf(
        response, sizeof(response),
        "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n"
        "Transfer-Encoding: chunked\r\n\r\n"
        "%zx\r\n%.*s\r\n%zx\r\n%s\r\n0\r\n\r\n",
        split, (int)split, body, length - split, body + split);
    if (written < 0 || (size_t)written >= sizeof(response)) return 1;
    return finality_rpc_case(
        response, (size_t)written, LXP_OK, LXP_DAEMON_ANCHOR_FINAL);
}

static int finality_rpc_malformed_chunked(void)
{
    static const char response[] =
        "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n"
        "zz\r\n{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"0x00\"}\r\n0\r\n\r\n";
    return finality_rpc_case(
        response, sizeof(response) - 1U, LXP_ERR_IO,
        LXP_DAEMON_ANCHOR_INSTANT);
}

int main(int argc, char **argv)
{
    char parallel_path[64] = "/tmp/layerxd-parallel-XXXXXX";
    char serial_path[64] = "/tmp/layerxd-serial-XXXXXX";
    char invalid_path[64] = "/tmp/layerxd-invalid-XXXXXX";
    char negative_path[64] = "/tmp/layerxd-negative-XXXXXX";
    char legacy_path[64] = "/tmp/layerxd-legacy-XXXXXX";
    char obsolete_path[64] = "/tmp/layerxd-obsolete-XXXXXX";
    lxp_daemon_configuration legacy;
    lxp_daemon_configuration parallel;
    lxp_daemon_configuration serial;
    lxp_daemon_configuration invalid;
    lxp_daemon_role_kind role;
    static lxp_daemon daemon;
    apply_state durable;
    uint8_t parallel_root[32];
    uint8_t serial_root[32];
    uint8_t drained_root[32];
    uint64_t restart_sequence;

    if (argc == 3 && strcmp(argv[1], "--startup-failure") == 0) {
        if (strcmp(argv[2], "allocation") == 0)
            return startup_thread_failure_child(RLIMIT_AS);
        if (strcmp(argv[2], "thread") == 0)
            return startup_thread_failure_child(RLIMIT_NPROC);
        return 2;
    }
    REQUIRE(argc == 1, "arguments");

    REQUIRE(http_parse_cases() == 0, "http response cases");
    REQUIRE(http_oversize_header() == 0, "http header bound");
    REQUIRE(http_incremental_chunked() == 0, "chunked reassembly");
    REQUIRE(finality_rpc_chunked() == 0, "chunked finality rpc");
    REQUIRE(finality_rpc_malformed_chunked() == 0,
            "malformed chunked finality rpc");
    REQUIRE(config_cases() == 0, "configuration cases");
    REQUIRE(effective_worker_cases() == 0, "effective program workers");
    REQUIRE(start_refusals() == 0, "start refusals");
    REQUIRE(startup_thread_failure(RLIMIT_NPROC) == 0, "startup thread failure");
    REQUIRE(startup_thread_failure(RLIMIT_AS) == 0, "startup allocation failure");

    REQUIRE(write_config(
        parallel_path, "sequencer", 0U, 2U, false) == 0,
        "write parallel config");
    REQUIRE(write_config(
        serial_path, "sequencer", 0U, 0U, true) == 0,
        "write serial config");
    REQUIRE(write_config(
        invalid_path, "sequencer,guarantor", 0U, 0U, true) == 0,
        "write invalid config");
    REQUIRE(write_negative_sequence_config(negative_path) == 0,
            "write negative config");
    REQUIRE(lxp_daemon_config_load(parallel_path, &parallel) == LXP_OK,
            "load parallel config");
    REQUIRE(lxp_daemon_config_load(serial_path, &serial) == LXP_OK,
            "load serial config");
    REQUIRE(parallel.config_version == LXP_DAEMON_CONFIG_VERSION &&
            serial.config_version == LXP_DAEMON_CONFIG_VERSION &&
            parallel.verify_workers == 2U && serial.verify_workers == 0U,
            "supported configuration fields");
    REQUIRE(write_legacy_config(
        legacy_path, 2U, 0U, 0U, 0U, false) == 0,
        "write legacy config");
    REQUIRE(lxp_daemon_config_load(legacy_path, &legacy) == LXP_OK &&
            legacy.config_version == LXP_DAEMON_CONFIG_VERSION_LEGACY &&
            legacy.verify_workers == 2U,
            "deprecated legacy config");
    REQUIRE(write_legacy_config(
        obsolete_path, 2U, 2U, 2U, 1U, false) == 0,
        "write obsolete config");
    REQUIRE(lxp_daemon_config_load(obsolete_path, &invalid) ==
                LXP_ERR_VERSION_UNSUPPORTED,
            "reject obsolete pools");
    REQUIRE(lxp_daemon_role(&parallel, &role) == LXP_OK,
            "resolve role");
    REQUIRE(role == LXP_DAEMON_SEQUENCER &&
            parallel.role == LXP_DAEMON_SEQUENCER,
            "sequencer role");
    REQUIRE(!parallel.serial_execution && serial.serial_execution,
            "execution mode");
    REQUIRE(lxp_daemon_config_load(invalid_path, &invalid) ==
                LXP_ERR_NON_CANONICAL,
            "reject multiple roles");
    REQUIRE(lxp_daemon_config_load(negative_path, &invalid) ==
                LXP_ERR_NON_CANONICAL,
            "reject negative sequence");
    REQUIRE(run_window(&parallel, 5000U, parallel_root, true) == 0,
            "parallel window");
    REQUIRE(run_window(&serial, 5000U, serial_root, true) == 0,
            "serial window");
    REQUIRE(memcmp(parallel_root, serial_root, 32U) == 0,
            "deterministic root");
    invalid = parallel;
    invalid.verify_workers = LXP_DAEMON_MAX_VERIFY_WORKERS;
    REQUIRE(run_window(&invalid, 5000U, drained_root, false) == 0 &&
            memcmp(drained_root, parallel_root, 32U) == 0,
            "maximum setting orderly stop drains admitted work");
    invalid.verify_workers = 0U;
    REQUIRE(run_window(&invalid, 5000U, drained_root, false) == 0 &&
            memcmp(drained_root, parallel_root, 32U) == 0,
            "zero setting orderly stop drains admitted work");

    (void)memset(&durable, 0, sizeof(durable));
    REQUIRE(lxp_daemon_start(
        &daemon, &parallel, apply_activity, &durable) == LXP_OK,
        "durable start");
    REQUIRE(submit_range(&daemon, 0U, 6000U) == 0,
            "durable submit");
    REQUIRE(await_sequence(&daemon, 6000U) == 0,
            "durable await");
    REQUIRE(lxp_daemon_shutdown(&daemon) == LXP_OK,
            "durable shutdown");
    REQUIRE(durable.expected_sequence == 6000U &&
            daemon.executed_count == 6000U,
            "durable sequence");
    restart_sequence = daemon.next_sequence;
    parallel.start_sequence = restart_sequence;
    REQUIRE(restart_sequence == 6000U, "restart sequence");
    REQUIRE(lxp_daemon_start(
        &daemon, &parallel, apply_activity, &durable) == LXP_OK,
        "restart start");
    REQUIRE(submit_range(&daemon, restart_sequence, 4000U) == 0,
            "restart submit");
    REQUIRE(await_sequence(&daemon, 10000U) == 0,
            "restart await");
    REQUIRE(lxp_daemon_shutdown(&daemon) == LXP_OK,
            "restart shutdown");
    REQUIRE(durable.expected_sequence == 10000U &&
            daemon.next_sequence == 10000U &&
            daemon.executed_count == 4000U,
            "restart completion");
    REQUIRE(unlink(parallel_path) == 0 && unlink(serial_path) == 0 &&
            unlink(invalid_path) == 0 && unlink(negative_path) == 0 &&
            unlink(legacy_path) == 0 && unlink(obsolete_path) == 0,
            "cleanup");
    return 0;
}
