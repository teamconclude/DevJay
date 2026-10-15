#import "spaces_darwin.h"
#import <dlfcn.h>

// Desktop identification and restoration use runtime-loaded SkyLight functions. These are private
// APIs: missing symbols disable desktop restoration without preventing launch.
static int (*mainConnection)(void);
static CFArrayRef (*copyWindowSpaces)(int, int, CFArrayRef);
static CFArrayRef (*copyDisplaySpaces)(int);
static int (*getCompatID)(int, uint64_t);
static int (*setCompatID)(int, uint64_t, int);
static int (*setWorkspace)(int, uint32_t *, int, int);
static int (*moveManaged)(int, CFArrayRef, uint64_t);
static int connection;

BOOL dp_spaces_available(void) {
    static BOOL initialized, available;
    if (!initialized) {
        initialized = YES;
        void *lib = dlopen("/System/Library/PrivateFrameworks/SkyLight.framework/SkyLight", RTLD_LAZY);
        if (lib) {
            mainConnection = dlsym(lib, "SLSMainConnectionID");
            copyWindowSpaces = dlsym(lib, "SLSCopySpacesForWindows");
            copyDisplaySpaces = dlsym(lib, "SLSCopyManagedDisplaySpaces");
            getCompatID = dlsym(lib, "SLSSpaceGetCompatID");
            setCompatID = dlsym(lib, "SLSSpaceSetCompatID");
            setWorkspace = dlsym(lib, "SLSSetWindowListWorkspace");
            moveManaged = dlsym(lib, "SLSMoveWindowsToManagedSpace");
            available = mainConnection && copyWindowSpaces && copyDisplaySpaces && ((getCompatID && setCompatID && setWorkspace) || moveManaged);
            if (available) connection = mainConnection();
        }
        NSLog(@"DevJay: desktop restoration %@", available ? @"available (private SkyLight APIs)" : @"unavailable");
    }
    return available;
}

// All callers run on the AppKit thread. Share topology across one snapshot
// and nearby layout saves; a short lifetime also detects changes without events.
static NSArray *cachedDisplays;
static NSTimeInterval cachedAt;
void dp_invalidate_space_cache(void) {
    [cachedDisplays release]; cachedDisplays = nil;
}
static NSArray *display_spaces(void) {
    if (!dp_spaces_available()) return @[];
    NSTimeInterval now = NSProcessInfo.processInfo.systemUptime;
    if (!cachedDisplays || now - cachedAt >= 0.2) {
        CFArrayRef raw = copyDisplaySpaces(connection);
        dp_invalidate_space_cache();
        if (raw) cachedDisplays = (NSArray *)raw;
        cachedAt = now;
    }
    return cachedDisplays ?: @[];
}
static NSArray *spaces(void) {
    NSMutableArray *result = [NSMutableArray array];
    for (NSDictionary *display in display_spaces()) {
        NSArray *items = display[@"Spaces"];
        if ([items isKindOfClass:NSArray.class]) [result addObjectsFromArray:items];
    }
    return result;
}

static NSString *space_at_index(NSArray *displays, NSString *identifier, NSInteger ordinal) {
    if (ordinal < 0) return nil;
    for (NSDictionary *display in displays) {
        if (![display[@"Display Identifier"] isEqual:identifier] && displays.count != 1) continue;
        NSInteger index = 0;
        for (NSDictionary *space in display[@"Spaces"]) {
            if ([space[@"type"] integerValue] != 0) continue;
            if (index++ == ordinal) return [space[@"uuid"] isKindOfClass:NSString.class] ? space[@"uuid"] : nil;
        }
    }
    return nil;
}
NSString *dp_space_for_index(NSString *display, NSInteger index) { return space_at_index(display_spaces(), display, index); }

NSString *dp_window_space(NSWindow *window) {
    if (!dp_spaces_available() || window.windowNumber <= 0 ||
        (window.collectionBehavior & NSWindowCollectionBehaviorCanJoinAllSpaces)) return nil;
    CFArrayRef raw = copyWindowSpaces(connection, 0x7, (CFArrayRef)@[@(window.windowNumber)]);
    if (!raw) return nil;
    uint64_t sid = CFArrayGetCount(raw) == 1 ? [(NSNumber *)CFArrayGetValueAtIndex(raw, 0) unsignedLongLongValue] : 0;
    CFRelease(raw);
    for (NSDictionary *space in spaces()) {
        if ([space[@"ManagedSpaceID"] unsignedLongLongValue] == sid &&
            [space[@"uuid"] isKindOfClass:NSString.class]) return space[@"uuid"];
    }
    return nil;
}

static BOOL move_with_compat_tag(uint64_t sid, uint32_t wid) {
    if (!getCompatID || !setCompatID || !setWorkspace) return NO;
    const int compat = 0x79616265;
    int previous = getCompatID(connection, sid);
    int prepare = setCompatID(connection, sid, compat);
    int move = prepare == 0 ? setWorkspace(connection, &wid, 1, compat) : -1;
    setCompatID(connection, sid, previous);
    return prepare == 0 && move == 0;
}

BOOL dp_move_to_space(NSWindow *window, NSString *uuid) {
    if (!dp_spaces_available() || window.windowNumber <= 0 || uuid.length == 0) return NO;
    dp_invalidate_space_cache();
    uint64_t sid = 0;
    for (NSDictionary *space in spaces()) {
        if ([space[@"uuid"] isEqual:uuid]) { sid = [space[@"ManagedSpaceID"] unsignedLongLongValue]; break; }
    }
    if (sid == 0) return NO; // Desktop was removed or its display is unavailable.
    uint32_t wid = (uint32_t)window.windowNumber;
    BOOL moved = move_with_compat_tag(sid, wid);
    // Return codes can report failure despite a successful move. Check membership
    // before attempting the older fallback, then verify again after settling.
    if (![dp_window_space(window) isEqual:uuid] && !moved && moveManaged) {
        moveManaged(connection, (CFArrayRef)@[@(wid)], sid);
    }
    return YES;
}

// Display position is read in AppKit coordinates; desktop rank follows the
// current Mission Control ordering within that display, not a saved Space ID.
char *devjay_desktop_position(void *pointer) {
    NSWindow *window = (NSWindow *)pointer;
    NSScreen *screen = window.screen;
    NSString *uuid = dp_window_space(window);
    NSInteger rank = -1;
    if (uuid && dp_spaces_available()) {
        for (NSDictionary *display in display_spaces()) {
            NSInteger index = 0;
            for (NSDictionary *space in display[@"Spaces"]) {
                if ([space[@"type"] integerValue] != 0) continue;
                if ([space[@"uuid"] isEqual:uuid]) rank = index;
                index++;
            }
        }
    }
    NSDictionary *value = @{@"known": @(screen != nil && rank >= 0),
        @"monitorX": @(screen.frame.origin.x), @"monitorY": @(screen.frame.origin.y),
        @"desktop": @(rank), @"x": @(window.frame.origin.x), @"y": @(window.frame.origin.y),
        @"label": rank < 0 ? @"Desktop unknown" : [NSString stringWithFormat:@"%@ · Desktop %ld", screen.localizedName ?: @"Display", (long)rank + 1]};
    NSData *data = [NSJSONSerialization dataWithJSONObject:value options:0 error:nil];
    NSString *json = data ? [[[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding] autorelease] : nil;
    return json.UTF8String ? strdup(json.UTF8String) : NULL;
}
