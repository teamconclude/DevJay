#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>
extern void devjay_editor_opened(unsigned long long token, const char *error);

static NSDictionary *editor_info(NSURL *url, BOOL allowOther) {
    if (!url || ![url.pathExtension.lowercaseString isEqualToString:@"app"]) return nil;
    NSBundle *bundle = [NSBundle bundleWithURL:url];
    NSString *identifier = bundle.bundleIdentifier;
    if (!identifier || !bundle.executablePath) return nil;
    NSSet *known = [NSSet setWithArray:@[@"com.microsoft.VSCode", @"com.microsoft.VSCodeInsiders", @"com.vscodium", @"dev.zed.Zed", @"dev.zed.Zed-Preview", @"com.todesktop.230313mzl4w4u92", @"com.exafunction.windsurf", @"com.google.antigravity", @"com.sublimetext.4", @"com.sublimetext.3", @"com.barebones.bbedit", @"com.panic.Nova", @"com.macromates.TextMate"]];
    // Product metadata distinguishes IDE bundles from Toolbox and JCEF helpers.
    BOOL jetbrains = [identifier.lowercaseString hasPrefix:@"com.jetbrains."] &&
        [NSFileManager.defaultManager fileExistsAtPath:[url.path stringByAppendingPathComponent:@"Contents/Resources/product-info.json"]];
    if (!allowOther && !jetbrains && ![known containsObject:identifier]) return nil;
    NSString *path = url.URLByResolvingSymlinksInPath.path;
    NSMutableArray *pids = [NSMutableArray array];
    for (NSRunningApplication *app in NSWorkspace.sharedWorkspace.runningApplications) {
        if ([app.bundleURL.URLByResolvingSymlinksInPath.path isEqualToString:path] && !app.terminated) [pids addObject:@(app.processIdentifier)];
    }
    NSString *name = [bundle objectForInfoDictionaryKey:@"CFBundleDisplayName"] ?: [bundle objectForInfoDictionaryKey:@"CFBundleName"] ?: url.lastPathComponent.stringByDeletingPathExtension;
    if ([identifier isEqualToString:@"com.microsoft.VSCode"]) name = @"Visual Studio Code";
    return @{@"name":name, @"path":path, @"bundleID":identifier, @"pids":pids};
}

char *devjay_editor_catalog(const char *extra) {
    @autoreleasepool {
        NSMutableDictionary *apps = [NSMutableDictionary dictionary];
        void (^add)(NSURL *, BOOL) = ^(NSURL *url, BOOL allowOther) {
            NSDictionary *info = editor_info(url, allowOther);
            if (info) apps[info[@"path"]] = info;
        };
        // Running Toolbox-managed IDEs may live outside the Applications folders.
        for (NSRunningApplication *app in NSWorkspace.sharedWorkspace.runningApplications) add(app.bundleURL, NO);
        for (NSString *root in @[@"/Applications", [NSHomeDirectory() stringByAppendingPathComponent:@"Applications"]]) {
            for (NSString *folder in @[root, [root stringByAppendingPathComponent:@"JetBrains"]]) {
                NSArray *urls = [NSFileManager.defaultManager contentsOfDirectoryAtURL:[NSURL fileURLWithPath:folder] includingPropertiesForKeys:nil options:NSDirectoryEnumerationSkipsHiddenFiles error:nil];
                for (NSURL *url in urls) add(url, NO);
            }
        }
        for (NSString *identifier in @[@"com.microsoft.VSCode", @"com.microsoft.VSCodeInsiders", @"com.vscodium", @"dev.zed.Zed", @"dev.zed.Zed-Preview", @"com.todesktop.230313mzl4w4u92", @"com.exafunction.windsurf", @"com.sublimetext.4", @"com.barebones.bbedit", @"com.panic.Nova", @"com.macromates.TextMate"]) add([NSWorkspace.sharedWorkspace URLForApplicationWithBundleIdentifier:identifier], NO);
        NSString *extraPath = extra ? [NSString stringWithUTF8String:extra] : nil;
        if (extraPath.length) add([NSURL fileURLWithPath:extraPath], YES);
        NSData *json = [NSJSONSerialization dataWithJSONObject:apps.allValues options:0 error:nil];
        return json ? strndup(json.bytes, json.length) : strdup("[]");
    }
}

void devjay_open_editor(const char *file, const char *app, unsigned long long token) {
    NSString *filePath = file ? [NSString stringWithUTF8String:file] : nil;
    NSString *appPath = app ? [NSString stringWithUTF8String:app] : nil;
    if (!filePath.length || !appPath.length) {
        devjay_editor_opened(token, "The file or editor path is not valid UTF-8. Open it manually.");
        return;
    }
    NSURL *fileURL = [NSURL fileURLWithPath:filePath];
    NSURL *appURL = [NSURL fileURLWithPath:appPath];
    NSWorkspaceOpenConfiguration *config = [NSWorkspaceOpenConfiguration configuration];
    config.activates = YES;
    config.createsNewApplicationInstance = NO;
    [NSWorkspace.sharedWorkspace openURLs:@[fileURL] withApplicationAtURL:appURL configuration:config completionHandler:^(NSRunningApplication *running, NSError *error) {
        devjay_editor_opened(token, error ? error.localizedDescription.UTF8String : (!running ? "The editor could not be opened." : ""));
    }];
}
