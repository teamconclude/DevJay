#import "site_apps_darwin.h"
#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
#import <dispatch/dispatch.h>
#import <dlfcn.h>
#include <stdlib.h>
#include <string.h>

// Keep the background reader independent of the main-thread Space/layout cache.
// Only the two read-only symbols are needed; missing move APIs do not matter.
static int (*site_apps_connection)(void);
static CFArrayRef (*site_apps_copy_spaces)(int, int, CFArrayRef);
static void site_apps_load_symbols(void) {
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        void *library = dlopen("/System/Library/PrivateFrameworks/SkyLight.framework/SkyLight", RTLD_LAZY);
        if (library) {
            site_apps_connection = dlsym(library, "SLSMainConnectionID");
            site_apps_copy_spaces = dlsym(library, "SLSCopySpacesForWindows");
        }
    });
}

unsigned int devjay_site_app_window_number(void *pointer) {
    NSWindow *window = (NSWindow *)pointer;
    NSInteger number = window.windowNumber;
    return number > 0 && (NSUInteger)number <= UINT32_MAX ? (unsigned int)number : 0;
}

static NSDictionary *site_app_identity(NSString *identifier, NSString *name, BOOL jetBrainsProduct) {
    if (!identifier.length) return nil;
    // Match main application bundles exactly, never their helper processes or
    // URL handlers. The unified ChatGPT app retains Codex's bundle identifier.
    NSDictionary *assistants = @{
        @"com.openai.codex": [name isEqualToString:@"ChatGPT"] ? @"ChatGPT" : @"Codex",
        @"com.anthropic.claudefordesktop": @"Claude"
    };
    NSDictionary *terminals = @{
        @"com.googlecode.iterm2": @"iTerm2", @"com.apple.Terminal": @"Terminal",
        @"com.mitchellh.ghostty": @"Ghostty", @"dev.warp.Warp-Stable": @"Warp",
        @"net.kovidgoyal.kitty": @"kitty", @"org.alacritty": @"Alacritty",
        @"com.github.wez.wezterm": @"WezTerm"
    };
    NSDictionary *editors = @{
        @"com.microsoft.VSCode": @"VS Code", @"com.microsoft.VSCodeInsiders": @"VS Code Insiders",
        @"com.microsoft.visual-studio": @"Visual Studio", @"com.vscodium": @"VSCodium",
        @"dev.zed.Zed": @"Zed", @"dev.zed.Zed-Preview": @"Zed Preview",
        @"com.todesktop.230313mzl4w4u92": @"Cursor", @"com.exafunction.windsurf": @"Windsurf",
        @"com.google.antigravity": @"Antigravity", @"com.sublimetext.4": @"Sublime Text",
        @"com.sublimetext.3": @"Sublime Text", @"com.barebones.bbedit": @"BBEdit",
        @"com.panic.Nova": @"Nova", @"com.macromates.TextMate": @"TextMate",
        @"com.apple.dt.Xcode": @"Xcode"
    };
    NSString *displayName = assistants[identifier] ?: terminals[identifier] ?: editors[identifier];
    BOOL terminal = terminals[identifier] != nil;
    if (!displayName && jetBrainsProduct && [identifier.lowercaseString hasPrefix:@"com.jetbrains."]) {
        displayName = name.length ? name : @"JetBrains IDE";
    }
    if (!displayName) return nil;
    return @{@"name": displayName, @"bundleID": identifier, @"kind": assistants[identifier] ? @"assistant" : terminal ? @"terminal" : @"editor"};
}

// Render only an installed app's icon, never window contents. Keep AppKit image
// access on the main thread and cache the small result for subsequent scans.
static NSString *site_app_icon_png(NSImage *image) {
    if (!image || image.size.width <= 0 || image.size.height <= 0) return nil;
    NSBitmapImageRep *bitmap = [[[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL
        pixelsWide:40 pixelsHigh:40 bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO
        colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:0 bitsPerPixel:0] autorelease];
    if (!bitmap || !bitmap.bitmapData) return nil;
    memset(bitmap.bitmapData, 0, bitmap.bytesPerRow * bitmap.pixelsHigh);
    NSGraphicsContext *context = [NSGraphicsContext graphicsContextWithBitmapImageRep:bitmap];
    if (!context) return nil;
    [NSGraphicsContext saveGraphicsState];
    [NSGraphicsContext setCurrentContext:context];
    context.imageInterpolation = NSImageInterpolationHigh;
    [image drawInRect:NSMakeRect(0, 0, 40, 40) fromRect:NSZeroRect
        operation:NSCompositingOperationCopy fraction:1 respectFlipped:YES hints:nil];
    [NSGraphicsContext restoreGraphicsState];
    NSData *png = [bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
    if (!png.length || png.length > 16384) return nil;
    return [@"data:image/png;base64," stringByAppendingString:[png base64EncodedStringWithOptions:0]];
}

static NSString *site_app_icon(NSURL *bundleURL) {
    if (!bundleURL.isFileURL || !bundleURL.path.isAbsolutePath) return nil;
    NSDictionary *attributes = [NSFileManager.defaultManager attributesOfItemAtPath:bundleURL.path error:nil];
    if (![attributes[NSFileType] isEqual:NSFileTypeDirectory]) return nil;
    NSString *key = [NSString stringWithFormat:@"%@|%@", bundleURL.path, attributes[NSFileModificationDate]];
    static NSCache *icons;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        icons = [[NSCache alloc] init];
        icons.countLimit = 64;
        icons.totalCostLimit = 2 * 1024 * 1024;
    });
    NSString *cached = [[icons objectForKey:key] retain];
    if (cached) return [cached autorelease];
    __block NSString *icon = nil;
    void (^readIcon)(void) = ^{
        @autoreleasepool {
            icon = [site_app_icon_png([NSWorkspace.sharedWorkspace iconForFile:bundleURL.path]) copy];
        }
    };
    if (NSThread.isMainThread) readIcon();
    else dispatch_sync(dispatch_get_main_queue(), readIcon);
    // Cache failures too; a missing icon should not add work to every scan.
    [icons setObject:icon ?: @"" forKey:key cost:icon.length];
    return [icon autorelease];
}

static BOOL site_app_bounds(NSDictionary *window, CGRect *bounds) {
    id value = window[(id)kCGWindowBounds];
    return [value isKindOfClass:NSDictionary.class] &&
        CGRectMakeWithDictionaryRepresentation((CFDictionaryRef)value, bounds) &&
        !CGRectIsNull(*bounds) && !CGRectIsInfinite(*bounds) &&
        isfinite(bounds->origin.x) && isfinite(bounds->origin.y) &&
        isfinite(bounds->size.width) && isfinite(bounds->size.height);
}

// Use the same WindowServer coordinate system for windows and physical displays.
// Largest overlap separates monitors even when they share a single Space.
static NSNumber *site_app_display(CGRect bounds, NSArray *displays) {
    NSNumber *best = nil;
    CGFloat largest = 0;
    for (NSDictionary *display in displays) {
        CGRect frame;
        if (!site_app_bounds(display, &frame)) continue;
        CGRect intersection = CGRectIntersection(bounds, frame);
        CGFloat area = CGRectIsNull(intersection) ? 0 : intersection.size.width * intersection.size.height;
        if (area > largest) { largest = area; best = display[@"id"]; }
    }
    return best;
}

// Kept separate from native enumeration so ambiguity, display matching, and
// stale/missing window metadata can be tested without touching real windows.
static NSDictionary *site_apps_match(NSDictionary *targets, NSArray *windows, NSArray *displays,
    NSDictionary *identities, uint64_t (^spaceForWindow)(NSNumber *)) {
    NSMutableDictionary *windowsByID = [NSMutableDictionary dictionary];
    for (NSDictionary *window in windows) {
        NSNumber *number = window[(id)kCGWindowNumber];
        if ([number isKindOfClass:NSNumber.class]) windowsByID[number] = window;
    }
    NSMutableDictionary *targetSpaces = [NSMutableDictionary dictionary];
    NSMutableDictionary *targetDisplays = [NSMutableDictionary dictionary];
    NSMutableDictionary *matches = [NSMutableDictionary dictionary];
    for (id site in targets) {
        id number = targets[site];
        if (![site isKindOfClass:NSString.class] || ![number isKindOfClass:NSNumber.class] ||
            [number unsignedLongLongValue] == 0 || [number unsignedLongLongValue] > UINT32_MAX ||
            [number doubleValue] != [number unsignedLongLongValue]) continue;
        NSDictionary *window = windowsByID[number];
        CGRect bounds;
        if (!window || !site_app_bounds(window, &bounds)) continue;
        NSNumber *display = site_app_display(bounds, displays);
        uint64_t space = display ? spaceForWindow(number) : 0;
        if (!space) continue;
        targetSpaces[site] = @(space);
        targetDisplays[site] = display;
        matches[site] = [NSMutableDictionary dictionary];
    }
    for (NSDictionary *window in windows) {
        NSDictionary *identity = identities[window[(id)kCGWindowOwnerPID]];
        CGRect bounds;
        if (!identity || [window[(id)kCGWindowLayer] integerValue] != 0 ||
            [window[(id)kCGWindowAlpha] doubleValue] <= 0 || !site_app_bounds(window, &bounds) ||
            bounds.size.width < 80 || bounds.size.height < 60) continue;
        NSNumber *display = site_app_display(bounds, displays);
        NSNumber *number = window[(id)kCGWindowNumber];
        uint64_t space = display && [number isKindOfClass:NSNumber.class] ? spaceForWindow(number) : 0;
        if (!space) continue;
        for (NSString *site in targetSpaces) {
            if ([targetSpaces[site] unsignedLongLongValue] != space || ![targetDisplays[site] isEqual:display]) continue;
            NSMutableDictionary *apps = matches[site];
            NSString *bundle = identity[@"bundleID"];
            NSMutableDictionary *app = apps[bundle];
            if (!app) {
                app = [[identity mutableCopy] autorelease];
                app[@"windowCount"] = @0;
                app[@"processIDs"] = [NSMutableArray array];
                apps[bundle] = app;
            }
            app[@"windowCount"] = @([app[@"windowCount"] unsignedIntegerValue] + 1);
            NSNumber *pid = window[(id)kCGWindowOwnerPID];
            if (![app[@"processIDs"] containsObject:pid]) [app[@"processIDs"] addObject:pid];
        }
    }
    NSMutableDictionary *result = [NSMutableDictionary dictionary];
    for (NSString *site in matches) {
        result[site] = [[matches[site] allValues] sortedArrayUsingComparator:^NSComparisonResult(NSDictionary *left, NSDictionary *right) {
            NSComparisonResult kind = [left[@"kind"] compare:right[@"kind"]];
            if (kind != NSOrderedSame) return kind;
            NSComparisonResult name = [left[@"name"] localizedCaseInsensitiveCompare:right[@"name"]];
            return name == NSOrderedSame ? [left[@"bundleID"] compare:right[@"bundleID"]] : name;
        }];
    }
    return result;
}

static char *site_apps_json(BOOL available, NSDictionary *sites) {
    NSData *json = [NSJSONSerialization dataWithJSONObject:@{@"available": @(available), @"sites": sites ?: @{}}
        options:0 error:nil];
    return json ? strndup(json.bytes, json.length) : strdup("{\"available\":false,\"sites\":{}}");
}

char *devjay_site_apps(const char *targetsJSON) {
    @autoreleasepool {
        site_apps_load_symbols();
        NSString *input = targetsJSON ? [NSString stringWithUTF8String:targetsJSON] : nil;
        NSData *data = [input dataUsingEncoding:NSUTF8StringEncoding];
        id targets = data ? [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] : nil;
        if (!site_apps_connection || !site_apps_copy_spaces || ![targets isKindOfClass:NSDictionary.class])
            return site_apps_json(NO, nil);
        // This API reads metadata only. It does not request Screen Recording,
        // Accessibility, Automation, or window pixels, including on other Spaces.
        NSArray *windows = [(NSArray *)CGWindowListCopyWindowInfo(kCGWindowListOptionAll | kCGWindowListExcludeDesktopElements,
            kCGNullWindowID) autorelease];
        CGDirectDisplayID displayIDs[64];
        uint32_t displayCount = 0;
        if (!windows.count || CGGetActiveDisplayList(64, displayIDs, &displayCount) != kCGErrorSuccess || !displayCount)
            return site_apps_json(NO, nil);
        NSMutableArray *displays = [NSMutableArray array];
        for (uint32_t i = 0; i < displayCount; i++) {
            NSDictionary *bounds = [(NSDictionary *)CGRectCreateDictionaryRepresentation(CGDisplayBounds(displayIDs[i])) autorelease];
            if (bounds) [displays addObject:@{@"id": @(displayIDs[i]), (id)kCGWindowBounds: bounds}];
        }
        NSMutableDictionary *identities = [NSMutableDictionary dictionary];
        NSMutableSet *seenPIDs = [NSMutableSet set];
        for (NSDictionary *window in windows) {
            NSNumber *pid = window[(id)kCGWindowOwnerPID];
            if (![pid isKindOfClass:NSNumber.class] || [seenPIDs containsObject:pid]) continue;
            [seenPIDs addObject:pid];
            NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid.intValue];
            if (!app || app.terminated) continue;
            NSString *identifier = app.bundleIdentifier;
            BOOL jetBrainsProduct = [identifier.lowercaseString hasPrefix:@"com.jetbrains."] &&
                [NSFileManager.defaultManager fileExistsAtPath:[app.bundleURL.path stringByAppendingPathComponent:@"Contents/Resources/product-info.json"]];
            NSDictionary *identity = site_app_identity(identifier, app.localizedName, jetBrainsProduct);
            if (identity) {
                NSMutableDictionary *observed = [[identity mutableCopy] autorelease];
                NSString *icon = site_app_icon(app.bundleURL);
                if (icon.length) observed[@"icon"] = icon;
                identities[pid] = observed;
            }
        }
        int connection = site_apps_connection();
        NSMutableDictionary *spaces = [NSMutableDictionary dictionary];
        uint64_t (^spaceForWindow)(NSNumber *) = ^uint64_t(NSNumber *number) {
            NSNumber *cached = spaces[number];
            if (cached) return cached.unsignedLongLongValue;
            CFArrayRef raw = site_apps_copy_spaces(connection, 0x7, (CFArrayRef)@[number]);
            uint64_t value = 0;
            if (raw) {
                if (CFArrayGetCount(raw) == 1) {
                    id member = (id)CFArrayGetValueAtIndex(raw, 0);
                    if ([member isKindOfClass:NSNumber.class]) value = [member unsignedLongLongValue];
                }
                CFRelease(raw);
            }
            spaces[number] = @(value);
            return value;
        };
        NSDictionary *result = site_apps_match(targets, windows, displays, identities, spaceForWindow);
        return site_apps_json(YES, result);
    }
}
