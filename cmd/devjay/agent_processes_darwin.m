#import "agent_processes_darwin.h"
#import <Foundation/Foundation.h>
#include <errno.h>
#include <libproc.h>
#include <stdlib.h>
#include <string.h>
#include <sys/proc.h>
#include <sys/proc_info.h>
#include <sys/sysctl.h>
#include <unistd.h>

static BOOL agent_process_version(NSString *value) {
    if (!value.length) return NO;
    NSArray *parts = [value componentsSeparatedByString:@"."];
    if (parts.count < 2) return NO;
    NSCharacterSet *notDigits = [[NSCharacterSet characterSetWithCharactersInString:@"0123456789"] invertedSet];
    for (NSString *part in parts) {
        if (!part.length || [part rangeOfCharacterFromSet:notDigits].location != NSNotFound) return NO;
    }
    return YES;
}

static NSString *agent_process_name(NSString *path, NSString *home) {
    if (!path.isAbsolutePath) return nil;
    NSString *name = path.lastPathComponent;
    if ([name isEqualToString:@"claude"]) return @"Claude Code";
    if ([name isEqualToString:@"codex"]) return @"Codex";
    // Claude's native installer uses a version-named binary behind its symlink.
    // proc_pidpath resolves that symlink, so checking only `claude` misses it.
    NSString *versions = [home stringByAppendingPathComponent:@".local/share/claude/versions"];
    if (home.isAbsolutePath && [path.stringByDeletingLastPathComponent isEqualToString:versions] &&
        agent_process_version(name)) return @"Claude Code";
    return nil;
}

static NSString *agent_process_host_bundle(NSString *path, NSString *home) {
    if (!home.isAbsolutePath || !path.isAbsolutePath) return nil;
    NSString *directory = [home stringByAppendingPathComponent:@"Library/Application Support/iTerm2"];
    NSString *name = path.lastPathComponent;
    NSString *prefix = @"iTermServer-";
    if ([path.stringByDeletingLastPathComponent isEqualToString:directory] &&
        [name hasPrefix:prefix] && agent_process_version([name substringFromIndex:prefix.length])) {
        // The persistent server can outlive/reparent away from iTerm's GUI.
        // This identifies the host family; it does not identify a terminal tab.
        return @"com.googlecode.iterm2";
    }
    return nil;
}

// A single process table is also able to follow root-owned login wrappers.
// libproc alone refuses metadata for those wrappers on an ordinary user app.
static NSDictionary *agent_process_table(void) {
    int mib[] = {CTL_KERN, KERN_PROC, KERN_PROC_ALL};
    for (int attempt = 0; attempt < 3; attempt++) {
        size_t size = 0;
        if (sysctl(mib, 3, NULL, &size, NULL, 0) != 0 || size == 0 || size > 32 * 1024 * 1024) return nil;
        size += 32 * sizeof(struct kinfo_proc);
        struct kinfo_proc *processes = calloc(1, size);
        if (!processes) return nil;
        if (sysctl(mib, 3, processes, &size, NULL, 0) != 0) {
            int code = errno;
            free(processes);
            if (code == ENOMEM) continue;
            return nil;
        }
        NSMutableDictionary *table = [NSMutableDictionary dictionary];
        for (size_t i = 0; i < size / sizeof(*processes); i++) {
            struct kinfo_proc *process = &processes[i];
            if (process->kp_proc.p_pid <= 1) continue;
            uint64_t started = (uint64_t)process->kp_proc.p_starttime.tv_sec * 1000000 + process->kp_proc.p_starttime.tv_usec;
            table[@(process->kp_proc.p_pid)] = @{@"parent": @(process->kp_eproc.e_ppid),
                @"uid": @(process->kp_eproc.e_ucred.cr_uid), @"started": @(started)};
        }
        free(processes);
        return table;
    }
    return nil;
}

static NSArray *agent_process_ancestors(NSNumber *pid, NSDictionary *table, uid_t user) {
    NSMutableArray *ancestors = [NSMutableArray array];
    NSMutableSet *seen = [NSMutableSet setWithObject:pid];
    NSDictionary *child = table[pid];
    if (!child || [child[@"uid"] unsignedIntValue] != user) return @[];
    for (int depth = 0; depth < 64; depth++) {
        NSNumber *parentPID = child[@"parent"];
        if (parentPID.intValue <= 1) return ancestors;
        if ([seen containsObject:parentPID]) return @[];
        NSDictionary *parent = table[parentPID];
        if (!parent) return ancestors;
        uid_t owner = [parent[@"uid"] unsignedIntValue];
        if ((owner != user && owner != 0) ||
            [parent[@"started"] unsignedLongLongValue] > [child[@"started"] unsignedLongLongValue]) return ancestors;
        [seen addObject:parentPID];
        [ancestors addObject:parentPID];
        child = parent;
    }
    return @[];
}

static NSString *agent_process_path(pid_t pid) {
    char path[PROC_PIDPATHINFO_MAXSIZE] = {0};
    if (proc_pidpath(pid, path, sizeof(path)) <= 0) return nil;
    path[sizeof(path) - 1] = 0;
    return [NSString stringWithUTF8String:path];
}

static BOOL agent_process_unchanged(pid_t pid, NSDictionary *record, uid_t user) {
    struct proc_bsdinfo info = {0};
    if (proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info)) != sizeof(info)) return NO;
    uint64_t started = info.pbi_start_tvsec * 1000000 + info.pbi_start_tvusec;
    return info.pbi_uid == user && info.pbi_ppid == [record[@"parent"] unsignedIntValue] &&
        started == [record[@"started"] unsignedLongLongValue];
}

static NSDictionary *agent_process_observation(NSNumber *pid, NSDictionary *table, NSString *home, uid_t user) {
    NSDictionary *record = table[pid];
    if (!record || [record[@"uid"] unsignedIntValue] != user) return nil;
    NSString *name = agent_process_name(agent_process_path(pid.intValue), home);
    if (!name) return nil;
    NSArray *ancestors = agent_process_ancestors(pid, table, user);
    if (!ancestors.count) return nil;
    struct proc_vnodepathinfo info = {0};
    if (proc_pidinfo(pid.intValue, PROC_PIDVNODEPATHINFO, 0, &info, sizeof(info)) != sizeof(info)) return nil;
    info.pvi_cdir.vip_path[sizeof(info.pvi_cdir.vip_path) - 1] = 0;
    NSString *cwd = [NSString stringWithUTF8String:info.pvi_cdir.vip_path];
    if (!cwd.isAbsolutePath || !agent_process_unchanged(pid.intValue, record, user)) return nil;
    NSMutableDictionary *result = [NSMutableDictionary dictionaryWithDictionary:@{
        @"name": name, @"pid": pid, @"started": record[@"started"], @"cwd": cwd, @"ancestors": ancestors}];
    for (NSNumber *parentPID in ancestors) {
        NSDictionary *parent = table[parentPID];
        if (!agent_process_unchanged(parentPID.intValue, parent, user)) continue;
        NSString *bundle = agent_process_host_bundle(agent_process_path(parentPID.intValue), home);
        if (bundle) { result[@"hostBundleID"] = bundle; break; }
    }
    return result;
}

static char *agent_process_json(NSArray *results) {
    NSData *json = [NSJSONSerialization dataWithJSONObject:results options:0 error:nil];
    return json ? strndup(json.bytes, json.length) : strdup("[]");
}

char *devjay_agent_processes(void) {
    @autoreleasepool {
        NSDictionary *table = agent_process_table();
        NSMutableArray *results = [NSMutableArray array];
        uid_t user = getuid();
        NSString *home = NSHomeDirectory().stringByResolvingSymlinksInPath;
        for (NSNumber *pid in table) {
            NSDictionary *result = agent_process_observation(pid, table, home, user);
            if (result) [results addObject:result];
        }
        return agent_process_json(results);
    }
}

// Known-agent refresh avoids querying executable paths for every user process.
// A fresh kernel table still validates identities and the entire parent chain.
// Changed identities/cwd/ancestry are dropped, causing full discovery in Go.
char *devjay_refresh_agent_processes(const char *knownJSON) {
    @autoreleasepool {
        NSString *input = knownJSON ? [NSString stringWithUTF8String:knownJSON] : nil;
        NSData *data = [input dataUsingEncoding:NSUTF8StringEncoding];
        id known = data ? [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] : nil;
        if (![known isKindOfClass:NSArray.class] || [known count] > 1024) return strdup("[]");
        NSDictionary *table = agent_process_table();
        NSMutableArray *results = [NSMutableArray array];
        uid_t user = getuid();
        NSString *home = NSHomeDirectory().stringByResolvingSymlinksInPath;
        for (id previous in known) {
            if (![previous isKindOfClass:NSDictionary.class]) continue;
            NSNumber *pid = previous[@"pid"];
            if (![pid isKindOfClass:NSNumber.class] || pid.intValue <= 1) continue;
            NSDictionary *current = agent_process_observation(pid, table, home, user);
            if (!current) continue;
            BOOL same = YES;
            for (NSString *key in @[@"name", @"pid", @"started", @"cwd", @"ancestors"]) {
                if (![current[key] isEqual:previous[key]]) { same = NO; break; }
            }
            id previousHost = previous[@"hostBundleID"], currentHost = current[@"hostBundleID"];
            if (!currentHost) currentHost = @"";
            if (!previousHost) previousHost = @"";
            if (same && [currentHost isEqual:previousHost]) [results addObject:current];
        }
        return agent_process_json(results);
    }
}

// Only our own short-lived hook executable may introduce a permission request.
// Verify the peer identity before and after walking to its nearest live Claude
// ancestor. This is metadata inspection, not UI/key-event injection.
char *devjay_claude_hook_process(int peer) {
    @autoreleasepool {
        uid_t user = getuid();
        NSDictionary *table = agent_process_table();
        NSDictionary *record = table[@(peer)];
        if (!record || ![agent_process_path(peer) isEqual:agent_process_path(getpid())] ||
            !agent_process_unchanged(peer, record, user)) return NULL;
        NSString *home = NSHomeDirectory().stringByResolvingSymlinksInPath;
        for (NSNumber *ancestor in agent_process_ancestors(@(peer), table, user)) {
            NSDictionary *process = agent_process_observation(ancestor, table, home, user);
            if (![process[@"name"] isEqual:@"Claude Code"]) continue;
            if (!agent_process_unchanged(peer, record, user)) return NULL;
            NSMutableDictionary *peerInfo = [NSMutableDictionary dictionaryWithDictionary:process];
            // Keep the helper's original start across app reconnects. A later
            // idle transition proves that this old invocation has ended.
            peerInfo[@"hookStarted"] = record[@"started"];
            NSData *data = [NSJSONSerialization dataWithJSONObject:peerInfo options:0 error:nil];
            return data ? strndup(data.bytes, data.length) : NULL;
        }
        return NULL;
    }
}
