#import <Foundation/Foundation.h>
#import <CoreServices/CoreServices.h>
#include <stdint.h>

extern void devjayFilesystemChanged(uintptr_t identifier, char *path, int rescan);

typedef struct {
    FSEventStreamRef stream;
    uintptr_t identifier;
} DevJayFilesystemWatch;

static dispatch_queue_t DevJayFilesystemQueue(void) {
    static dispatch_queue_t queue;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        queue = dispatch_queue_create("io.teamconclude.devjay.filesystem", DISPATCH_QUEUE_SERIAL);
    });
    return queue;
}

static void DevJayFilesystemChanged(ConstFSEventStreamRef stream, void *context,
    size_t count, void *paths, const FSEventStreamEventFlags flags[],
    const FSEventStreamEventId identifiers[]) {
    (void)stream;
    (void)identifiers;
    @autoreleasepool {
        DevJayFilesystemWatch *watch = context;
        NSArray *names = (__bridge NSArray *)paths;
        const FSEventStreamEventFlags rescanFlags = kFSEventStreamEventFlagMustScanSubDirs |
            kFSEventStreamEventFlagUserDropped | kFSEventStreamEventFlagKernelDropped |
            kFSEventStreamEventFlagRootChanged | kFSEventStreamEventFlagMount |
            kFSEventStreamEventFlagUnmount;
        for (size_t i = 0; i < count; i++) {
            const char *path = [names[i] UTF8String];
            if (path) devjayFilesystemChanged(watch->identifier, (char *)path, (flags[i] & rescanFlags) != 0);
        }
    }
}

void *devjay_filesystem_watch(const char *paths, uintptr_t identifier) {
    @autoreleasepool {
        if (!paths) return NULL;
        NSData *data = [[NSString stringWithUTF8String:paths] dataUsingEncoding:NSUTF8StringEncoding];
        NSArray *names = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
        if (![names isKindOfClass:[NSArray class]] || names.count == 0) return NULL;
        for (id name in names) {
            if (![name isKindOfClass:[NSString class]] || ![name isAbsolutePath]) return NULL;
        }
        DevJayFilesystemWatch *watch = calloc(1, sizeof(*watch));
        if (!watch) return NULL;
        watch->identifier = identifier;
        FSEventStreamContext context = {0, watch, NULL, NULL, NULL};
        watch->stream = FSEventStreamCreate(NULL, DevJayFilesystemChanged, &context,
            (__bridge CFArrayRef)names, kFSEventStreamEventIdSinceNow, 0.25,
            kFSEventStreamCreateFlagUseCFTypes | kFSEventStreamCreateFlagFileEvents |
            kFSEventStreamCreateFlagWatchRoot);
        if (!watch->stream) { free(watch); return NULL; }
        FSEventStreamSetDispatchQueue(watch->stream, DevJayFilesystemQueue());
        if (!FSEventStreamStart(watch->stream)) {
            FSEventStreamInvalidate(watch->stream);
            FSEventStreamRelease(watch->stream);
            free(watch);
            return NULL;
        }
        return watch;
    }
}

void devjay_filesystem_watch_close(void *value) {
    DevJayFilesystemWatch *watch = value;
    if (!watch) return;
    FSEventStreamStop(watch->stream);
    FSEventStreamInvalidate(watch->stream);
    // No callback can retain the Go handle beyond this barrier.
    dispatch_sync(DevJayFilesystemQueue(), ^{});
    FSEventStreamRelease(watch->stream);
    free(watch);
}
