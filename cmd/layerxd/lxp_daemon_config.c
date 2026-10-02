#include "layerx/lxp_daemon.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const char *const version_two_keys[] = {
    "config_version=",
    "role=",
    "network_id=",
    "start_sequence=",
    "verify_workers=",
    "serial_execution=",
};

static const char *const legacy_pool_keys[] = {
    "network_workers=",
    "projection_workers=",
    "checkpoint_workers=",
};

static bool has_prefix(const char *line, const char *prefix)
{
    return strncmp(line, prefix, strlen(prefix)) == 0;
}

static void report_removed_pool(const char *line)
{
    size_t i;
    for (i = 0U; i < sizeof(legacy_pool_keys) / sizeof(legacy_pool_keys[0]); ++i) {
        if (has_prefix(line, legacy_pool_keys[i])) {
            (void)fprintf(stderr,
                "layerxd: config refused: %.*s has no execution owner; "
                "removed from config_version=2\n",
                (int)(strlen(legacy_pool_keys[i]) - 1U), legacy_pool_keys[i]);
            return;
        }
    }
}

static bool is_version_two_key(const char *line)
{
    size_t i;
    for (i = 0U; i < sizeof(version_two_keys) / sizeof(version_two_keys[0]);
         ++i)
        if (has_prefix(line, version_two_keys[i])) return true;
    return false;
}

static lxp_result parse_u64(
    const char *line, const char *prefix, uint64_t *value)
{
    char *end = NULL;
    unsigned long long parsed;
    size_t prefix_length = strlen(prefix);
    if (strncmp(line, prefix, prefix_length) != 0 ||
        line[prefix_length] == '\0')
        return LXP_ERR_NON_CANONICAL;
    if (line[prefix_length] < '0' || line[prefix_length] > '9')
        return LXP_ERR_NON_CANONICAL;
    errno = 0;
    parsed = strtoull(line + prefix_length, &end, 10);
    if (errno != 0 || end == line + prefix_length ||
        (*end != '\n' && *end != '\0'))
        return LXP_ERR_NON_CANONICAL;
    *value = (uint64_t)parsed;
    return LXP_OK;
}

static lxp_result parse_workers(
    const char *line, const char *prefix, size_t *workers)
{
    uint64_t value;
    lxp_result status = parse_u64(line, prefix, &value);
    if (status != LXP_OK || value > LXP_DAEMON_MAX_VERIFY_WORKERS)
        return LXP_ERR_LENGTH_LIMIT;
    *workers = (size_t)value;
    return LXP_OK;
}

static lxp_result read_line(FILE *file, char line[128])
{
    size_t length;
    if (fgets(line, 128, file) == NULL) return LXP_ERR_TRUNCATED;
    length = strlen(line);
    if (length == 0U || (length == 127U && line[126] != '\n'))
        return LXP_ERR_LENGTH_LIMIT;
    return LXP_OK;
}

static lxp_result expect_version_two_line(
    lxp_result status, FILE *file, char line[128], const char *key)
{
    if (status != LXP_OK) return status;
    status = read_line(file, line);
    if (status == LXP_OK && !has_prefix(line, key)) {
        report_removed_pool(line);
        status = is_version_two_key(line) ? LXP_ERR_NON_CANONICAL :
                                            LXP_ERR_UNKNOWN_FIELD;
    }
    return status;
}

static lxp_result parse_role(const char *line, lxp_daemon_role_kind *role)
{
    if (strcmp(line, "role=sequencer\n") == 0)
        *role = LXP_DAEMON_SEQUENCER;
    else if (strcmp(line, "role=replica\n") == 0)
        *role = LXP_DAEMON_REPLICA;
    else if (strcmp(line, "role=guarantor\n") == 0)
        *role = LXP_DAEMON_GUARANTOR;
    else return LXP_ERR_NON_CANONICAL;
    return LXP_OK;
}

static lxp_result parse_network_id(const char *line, uint32_t *network_id)
{
    uint64_t value;
    lxp_result status = parse_u64(line, "network_id=", &value);
    if (status == LXP_OK && (value == 0U || value > UINT32_MAX))
        status = LXP_ERR_WRONG_NETWORK;
    if (status == LXP_OK) *network_id = (uint32_t)value;
    return status;
}

static lxp_result parse_serial(const char *line, bool *serial_execution)
{
    if (strcmp(line, "serial_execution=true\n") == 0)
        *serial_execution = true;
    else if (strcmp(line, "serial_execution=false\n") == 0)
        *serial_execution = false;
    else return LXP_ERR_NON_CANONICAL;
    return LXP_OK;
}

static lxp_result load_version_two(
    FILE *file, char line[128], lxp_daemon_configuration *config)
{
    uint64_t version;
    lxp_result status = parse_u64(line, "config_version=", &version);
    if (status == LXP_OK && version != LXP_DAEMON_CONFIG_VERSION)
        status = LXP_ERR_VERSION_UNSUPPORTED;
    if (status == LXP_OK) config->config_version = LXP_DAEMON_CONFIG_VERSION;
    status = expect_version_two_line(status, file, line, "role=");
    if (status == LXP_OK) status = parse_role(line, &config->role);
    status = expect_version_two_line(status, file, line, "network_id=");
    if (status == LXP_OK) status = parse_network_id(line, &config->network_id);
    status = expect_version_two_line(status, file, line, "start_sequence=");
    if (status == LXP_OK)
        status = parse_u64(line, "start_sequence=", &config->start_sequence);
    status = expect_version_two_line(status, file, line, "verify_workers=");
    if (status == LXP_OK)
        status = parse_workers(line, "verify_workers=",
                               &config->verify_workers);
    status = expect_version_two_line(status, file, line, "serial_execution=");
    if (status == LXP_OK)
        status = parse_serial(line, &config->serial_execution);
    if (status == LXP_OK && fgets(line, 128, file) != NULL) {
        report_removed_pool(line);
        status = is_version_two_key(line) ? LXP_ERR_TRAILING_BYTES :
                                            LXP_ERR_UNKNOWN_FIELD;
    }
    return status;
}

static lxp_result load_version_one(
    FILE *file, char line[128], lxp_daemon_configuration *config)
{
    size_t pools[3] = {0U, 0U, 0U};
    size_t i;
    lxp_result status = parse_role(line, &config->role);
    if (status == LXP_OK) status = read_line(file, line);
    if (status == LXP_OK) status = parse_network_id(line, &config->network_id);
    if (status == LXP_OK) status = read_line(file, line);
    if (status == LXP_OK)
        status = parse_u64(line, "start_sequence=", &config->start_sequence);
    if (status == LXP_OK) status = read_line(file, line);
    if (status == LXP_OK)
        status = parse_workers(line, "verify_workers=",
                               &config->verify_workers);
    for (i = 0U; status == LXP_OK && i < 3U; ++i) {
        status = read_line(file, line);
        if (status == LXP_OK)
            status = parse_workers(line, legacy_pool_keys[i], &pools[i]);
    }
    if (status == LXP_OK) status = read_line(file, line);
    if (status == LXP_OK)
        status = parse_serial(line, &config->serial_execution);
    if (status == LXP_OK && fgets(line, 128, file) != NULL)
        status = LXP_ERR_TRAILING_BYTES;
    for (i = 0U; status == LXP_OK && i < 3U; ++i) {
        if (pools[i] != 0U) {
            (void)fprintf(stderr,
                "layerxd: config refused: %.*s=%zu has no execution owner; "
                "remove it and use config_version=2\n",
                (int)(strlen(legacy_pool_keys[i]) - 1U), legacy_pool_keys[i],
                pools[i]);
            status = LXP_ERR_VERSION_UNSUPPORTED;
        }
    }
    if (status == LXP_OK)
        config->config_version = LXP_DAEMON_CONFIG_VERSION_LEGACY;
    return status;
}

lxp_result lxp_daemon_config_load(
    const char *path, lxp_daemon_configuration *config)
{
    FILE *file;
    char line[128];
    lxp_result status;
    if (path == NULL || config == NULL) return LXP_ERR_NON_CANONICAL;
    file = fopen(path, "rb");
    if (file == NULL) return LXP_ERR_IO;
    (void)memset(config, 0, sizeof(*config));
    status = read_line(file, line);
    if (status == LXP_OK && has_prefix(line, "config_version="))
        status = load_version_two(file, line, config);
    else if (status == LXP_OK && has_prefix(line, "role="))
        status = load_version_one(file, line, config);
    else if (status == LXP_OK)
        status = LXP_ERR_UNKNOWN_FIELD;
    if (status == LXP_OK && config->serial_execution &&
        config->verify_workers != 0U)
        status = LXP_ERR_NON_CANONICAL;
    if (fclose(file) != 0 && status == LXP_OK) status = LXP_ERR_IO;
    if (status == LXP_OK &&
        config->config_version == LXP_DAEMON_CONFIG_VERSION_LEGACY)
        (void)fprintf(stderr,
            "layerxd: config deprecated: version 1 configuration accepted; "
            "rewrite as config_version=2 (network_workers, projection_workers "
            "and checkpoint_workers are removed)\n");
    if (status != LXP_OK) (void)memset(config, 0, sizeof(*config));
    return status;
}

lxp_result lxp_daemon_config(
    const char *path, lxp_daemon_configuration *config)
{
    return lxp_daemon_config_load(path, config);
}

lxp_result lxp_daemon_role(
    const lxp_daemon_configuration *config,
    lxp_daemon_role_kind *role)
{
    if (config == NULL || role == NULL ||
        config->role < LXP_DAEMON_SEQUENCER ||
        config->role > LXP_DAEMON_GUARANTOR)
        return LXP_ERR_NON_CANONICAL;
    *role = config->role;
    return LXP_OK;
}
