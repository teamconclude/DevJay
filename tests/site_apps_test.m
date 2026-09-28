#import <AppKit/AppKit.h>
#include <assert.h>
#include <stdio.h>
#include "../cmd/devjay/site_apps_darwin.m"

static NSDictionary *bounds(CGFloat x, CGFloat y, CGFloat width, CGFloat height) {
    return [(NSDictionary *)CGRectCreateDictionaryRepresentation(CGRectMake(x, y, width, height)) autorelease];
}

static NSMutableDictionary *window(unsigned int number, int pid, CGFloat x, CGFloat width) {
    return [@{(id)kCGWindowNumber: @(number), (id)kCGWindowOwnerPID: @(pid),
        (id)kCGWindowBounds: bounds(x, 50, width, 300), (id)kCGWindowLayer: @0,
        (id)kCGWindowAlpha: @1, (id)kCGWindowIsOnscreen: @NO} mutableCopy];
}

int main(void) {
    @autoreleasepool {
        assert(!site_app_icon_png(nil));
        assert(!site_app_icon([NSURL URLWithString:@"https://example.com/icon.png"]));
        NSBitmapImageRep *source = [[[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL
            pixelsWide:2 pixelsHigh:2 bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO
            colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:0 bitsPerPixel:0] autorelease];
        memset(source.bitmapData, 255, source.bytesPerRow * source.pixelsHigh);
        NSImage *sourceImage = [[[NSImage alloc] initWithSize:NSMakeSize(2, 2)] autorelease];
        [sourceImage addRepresentation:source];
        NSString *icon = site_app_icon_png(sourceImage);
        assert([icon hasPrefix:@"data:image/png;base64,"]);
        NSData *png = [[[NSData alloc] initWithBase64EncodedString:[icon substringFromIndex:22] options:0] autorelease];
        NSBitmapImageRep *decoded = [NSBitmapImageRep imageRepWithData:png];
        assert(decoded.pixelsWide == 40 && decoded.pixelsHigh == 40);
        assert(png.length <= 16384);

        assert([site_app_identity(@"com.microsoft.VSCode", @"Code", NO)[@"name"] isEqual:@"VS Code"]);
        assert([site_app_identity(@"com.googlecode.iterm2", @"iTerm2", NO)[@"kind"] isEqual:@"terminal"]);
        assert([site_app_identity(@"com.openai.codex", @"ChatGPT", NO)[@"name"] isEqual:@"ChatGPT"]);
        assert([site_app_identity(@"com.openai.codex", @"Codex", NO)[@"name"] isEqual:@"Codex"]);
        assert([site_app_identity(@"com.anthropic.claudefordesktop", @"Claude", NO)[@"kind"] isEqual:@"assistant"]);
        assert(!site_app_identity(@"com.openai.codex.helper", @"Codex Helper", NO));
        assert(!site_app_identity(@"com.anthropic.claudefordesktop.helper", @"Claude Helper", NO));
        assert(!site_app_identity(@"com.anthropic.claude-code-url-handler", @"Claude Code URL Handler", NO));
        assert(site_app_identity(@"com.jetbrains.goland", @"GoLand", YES));
        assert(!site_app_identity(@"com.jetbrains.toolbox", @"Toolbox", NO));
        assert(!site_app_identity(@"com.jetbrains.cefserver", @"cef_server", NO));
        assert(!site_app_identity(@"com.apple.Safari", @"Safari", NO));
        assert(!site_app_identity(nil, nil, NO));

        NSArray *displays = @[@{@"id": @1, (id)kCGWindowBounds: bounds(0, 0, 1000, 800)},
            @{@"id": @2, (id)kCGWindowBounds: bounds(1000, 0, 1000, 800)}];
        assert([site_app_display(CGRectMake(900, 0, 600, 300), displays) isEqual:@2]);
        assert(!site_app_display(CGRectMake(-1000, 0, 300, 300), displays));

        NSMutableArray *windows = [NSMutableArray array];
        for (NSNumber *number in @[@1, @2, @3, @4, @5]) {
            [windows addObject:[window(number.unsignedIntValue, 99, number.intValue == 2 ? 1100 : 50, 216) autorelease]];
        }
        [windows addObject:[window(11, 101, 50, 500) autorelease]];
        [windows addObject:[window(12, 101, 100, 400) autorelease]];
        [windows addObject:[window(13, 102, 50, 500) autorelease]];
        [windows addObject:[window(14, 103, 50, 500) autorelease]];
        [windows addObject:[window(15, 103, 1100, 500) autorelease]];
        [windows addObject:[window(16, 103, 50, 500) autorelease]];
        NSMutableDictionary *transparent = [window(17, 101, 50, 500) autorelease];
        transparent[(id)kCGWindowAlpha] = @0;
        [windows addObject:transparent];
        NSMutableDictionary *panel = [window(18, 101, 50, 500) autorelease];
        panel[(id)kCGWindowLayer] = @3;
        [windows addObject:panel];
        [windows addObject:[window(19, 101, 50, 1) autorelease]];
        [windows addObject:[window(20, 104, 50, 500) autorelease]];
        [windows addObject:[window(21, 105, 50, 500) autorelease]];
        [windows addObject:[window(22, 106, 50, 500) autorelease]];
        // 16 has ambiguous/all-Space membership. 5 is an unknown Site Space.
        NSDictionary *spaces = @{@1: @10, @2: @10, @3: @20, @4: @30, @5: @0,
            @11: @10, @12: @10, @13: @10, @14: @20, @15: @10, @16: @0,
            @17: @10, @18: @10, @19: @10, @20: @10, @21: @10, @22: @20};
        NSDictionary *identities = @{@101: site_app_identity(@"com.jetbrains.goland", @"GoLand", YES),
            @102: site_app_identity(@"com.googlecode.iterm2", @"iTerm2", NO),
            @103: site_app_identity(@"com.microsoft.VSCode", @"Code", NO),
            @105: site_app_identity(@"com.openai.codex", @"ChatGPT", NO),
            @106: site_app_identity(@"com.anthropic.claudefordesktop", @"Claude", NO)};
        NSDictionary *matches = site_apps_match(@{@"main": @1, @"side": @2, @"inactive": @3,
            @"empty": @4, @"unknown": @5, @"missing": @123, @"invalid": @"1", @"fraction": @1.5},
            windows, displays, identities, ^uint64_t(NSNumber *number) { return [spaces[number] unsignedLongLongValue]; });
        assert(matches.count == 4);
        assert([matches[@"main"] count] == 3);
        assert([matches[@"main"][0][@"name"] isEqual:@"ChatGPT"]);
        assert([matches[@"main"][1][@"name"] isEqual:@"GoLand"]);
        assert([matches[@"main"][1][@"windowCount"] intValue] == 2);
        assert([matches[@"main"][1][@"processIDs"] isEqual:@[@101]]);
        assert([matches[@"main"][2][@"name"] isEqual:@"iTerm2"]);
        assert([matches[@"side"] count] == 1);
        assert([matches[@"side"][0][@"name"] isEqual:@"VS Code"]);
        assert([matches[@"inactive"] count] == 2);
        assert([matches[@"inactive"][0][@"name"] isEqual:@"Claude"]);
        assert([matches[@"inactive"][1][@"name"] isEqual:@"VS Code"]);
        assert([matches[@"empty"] count] == 0);
        assert(!matches[@"unknown"] && !matches[@"missing"] && !matches[@"invalid"] && !matches[@"fraction"]);

        char *json = devjay_site_apps("\xff");
        assert(json && strstr(json, "\"available\":false"));
        free(json);
        puts("Site app metadata matching passed");
    }
    return 0;
}
