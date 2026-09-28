#import <Foundation/Foundation.h>
#include <assert.h>
#include <stdio.h>
#include "../cmd/devjay/agent_processes_darwin.m"

static NSDictionary *process(pid_t parent, uid_t uid, uint64_t started) {
    return @{@"parent": @(parent), @"uid": @(uid), @"started": @(started)};
}

int main(void) {
    @autoreleasepool {
        NSString *home = @"/Users/test";
        assert([agent_process_name(@"/opt/homebrew/bin/codex", home) isEqual:@"Codex"]);
        assert([agent_process_name(@"/opt/homebrew/lib/node_modules/@openai/codex/vendor/bin/codex", home) isEqual:@"Codex"]);
        assert([agent_process_name(@"/Users/test/.vscode/extensions/anthropic.claude-code-2.1.283/resources/native-binary/claude", home) isEqual:@"Claude Code"]);
        assert([agent_process_name(@"/Users/test/.local/share/claude/versions/2.1.283", home) isEqual:@"Claude Code"]);
        assert(!agent_process_name(@"/Users/test/.local/share/claude/versions/not-a-version", home));
        assert(!agent_process_name(@"/Users/test/.local/share/claude/versions/2..283", home));
        assert(!agent_process_name(@"/Users/test/.local/share/claude/versions/2.1.283/helper", home));
        assert(!agent_process_name(@"/Users/other/.local/share/claude/versions/2.1.283", home));
        assert(!agent_process_name(@"/tmp/codex-helper", home));
        assert(!agent_process_name(@"/tmp/Claude", home));
        assert(!agent_process_name(@"/tmp/node", home));
        assert(!agent_process_name(@"codex", home));
        assert(!agent_process_name(nil, home));

        assert([agent_process_host_bundle(@"/Users/test/Library/Application Support/iTerm2/iTermServer-3.7.2", home) isEqual:@"com.googlecode.iterm2"]);
        assert(!agent_process_host_bundle(@"/Users/test/Library/Application Support/iTerm2/sub/iTermServer-3.7.2", home));
        assert(!agent_process_host_bundle(@"/Users/test/Library/Application Support/iTerm2/iTermServer-3.7.2-helper", home));
        assert(!agent_process_host_bundle(@"/Users/test/Library/Application Support/iTerm2/iTermServer-3.7..2", home));
        assert(!agent_process_host_bundle(@"/Library/Application Support/iTerm2/iTermServer-3.7.2", home));
        assert(!agent_process_host_bundle(@"/Users/other/Library/Application Support/iTerm2/iTermServer-3.7.2", home));
        assert(!agent_process_host_bundle(nil, home));

        NSDictionary *table = @{@100: process(90, 501, 100), @90: process(80, 501, 90),
            @80: process(70, 0, 80), @70: process(1, 501, 70)};
        assert(([agent_process_ancestors(@100, table, 501) isEqual:@[@90, @80, @70]]));
        assert([agent_process_ancestors(@100, table, 502) count] == 0);
        assert([agent_process_ancestors(@101, table, 501) count] == 0);
        assert([agent_process_ancestors(@70, table, 501) count] == 0);
        assert([agent_process_ancestors(@100, @{@100: process(90, 501, 100)}, 501) count] == 0);
        assert([agent_process_ancestors(@100, @{@100: process(90, 501, 100), @90: process(1, 502, 90)}, 501) count] == 0);
        assert([agent_process_ancestors(@100, @{@100: process(90, 501, 100), @90: process(1, 501, 110)}, 501) count] == 0);
        assert([agent_process_ancestors(@100, @{@100: process(90, 501, 100), @90: process(100, 501, 100)}, 501) count] == 0);
        NSMutableDictionary *deep = [NSMutableDictionary dictionary];
        for (int pid = 2; pid < 100; pid++) deep[@(pid)] = process(pid - 1, 501, pid);
        assert([agent_process_ancestors(@99, deep, 501) count] == 0);

        puts("Agent process identity and ancestry checks passed");
    }
    return 0;
}
