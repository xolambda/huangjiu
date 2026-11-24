#include "core.h"

#include <string.h>
#include <unistd.h>
#include <sys/stat.h>

#ifdef __FreeBSD__
#include <sys/types.h>
#include <sys/sysctl.h>

static int
sysctl_string(const char *name, char *out, size_t out_len)
{
    size_t len = out_len;
    if (sysctlbyname(name, out, &len, NULL, 0) != 0)
        return -1;
    out[out_len - 1] = '\0';
    return 0;
}

int
huangjiu_get_sysinfo(huangjiu_sysinfo *out)
{
    if (out == NULL)
        return -1;
    memset(out, 0, sizeof(*out));

    size_t len = sizeof(out->total_memory_bytes);
    if (sysctlbyname("hw.physmem", &out->total_memory_bytes, &len, NULL, 0) != 0)
        return -1;

    unsigned int free_pages = 0;
    len = sizeof(free_pages);
    if (sysctlbyname("vm.stats.vm.v_free_count", &free_pages, &len, NULL, 0) == 0) {
        unsigned int page_size = 0;
        len = sizeof(page_size);
        if (sysctlbyname("hw.pagesize", &page_size, &len, NULL, 0) == 0) {
            out->page_size = page_size;
            out->free_memory_bytes = (uint64_t)free_pages * page_size;
        }
    }

    len = sizeof(out->ncpu);
    (void)sysctlbyname("hw.ncpu", &out->ncpu, &len, NULL, 0);
    (void)sysctl_string("hw.model", out->hw_model, sizeof(out->hw_model));
    (void)sysctl_string("kern.osrelease", out->os_release, sizeof(out->os_release));
    return 0;
}
#else
int
huangjiu_get_sysinfo(huangjiu_sysinfo *out)
{
    if (out != NULL)
        memset(out, 0, sizeof(*out));
    return -1;
}
#endif

int
huangjiu_is_executable(const char *path)
{
    struct stat st;
    if (path == NULL || access(path, X_OK) != 0 || stat(path, &st) != 0)
        return 0;
    return S_ISREG(st.st_mode) ? 1 : 0;
}
