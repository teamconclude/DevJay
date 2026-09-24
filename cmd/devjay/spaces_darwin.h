#import <AppKit/AppKit.h>
BOOL dp_spaces_available(void);
NSString *dp_window_space(NSWindow *window);
BOOL dp_move_to_space(NSWindow *window, NSString *uuid);

void dp_invalidate_space_cache(void);
