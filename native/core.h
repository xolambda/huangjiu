#ifndef HUANGJIU_CORE_H
#define HUANGJIU_CORE_H

#include <stdint.h>

typedef struct {
    uint64_t total_memory_bytes;
    uint64_t free_memory_bytes;
    uint64_t page_size;
    int ncpu;
    char hw_model[64];
    char os_release[64];
} huangjiu_sysinfo;

int huangjiu_get_sysinfo(huangjiu_sysinfo *out);
int huangjiu_is_executable(const char *path);

#endif
