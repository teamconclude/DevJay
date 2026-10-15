#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

char *devjay_folder_bookmark(const char *path) {
    @autoreleasepool {
        NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path] isDirectory:YES];
        NSData *data = [url bookmarkDataWithOptions:NSURLBookmarkCreationWithoutImplicitSecurityScope includingResourceValuesForKeys:nil relativeToURL:nil error:nil];
        return data ? strdup([data base64EncodedStringWithOptions:0].UTF8String) : NULL;
    }
}

char *devjay_folder_reference(const char *path) {
    @autoreleasepool {
        NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path] isDirectory:YES].fileReferenceURL;
        return url ? strdup(url.absoluteString.UTF8String) : NULL;
    }
}
char *devjay_resolve_folder_reference(const char *reference) {
    @autoreleasepool {
        NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:reference]];
        NSURL *path = url.isFileReferenceURL ? url.filePathURL : nil;
        return path ? strdup(path.fileSystemRepresentation) : NULL;
    }
}
char *devjay_folder_volume(const char *path) {
    @autoreleasepool {
        NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path] isDirectory:YES];
        NSString *uuid = nil;
        [url getResourceValue:&uuid forKey:NSURLVolumeUUIDStringKey error:nil];
        return [uuid isKindOfClass:NSString.class] ? strdup(uuid.UTF8String) : NULL;
    }
}

char *devjay_resolve_folder(const char *bookmark) {
    @autoreleasepool {
        NSData *data = [[[NSData alloc] initWithBase64EncodedString:[NSString stringWithUTF8String:bookmark] options:0] autorelease];
        if (!data) return NULL;
        BOOL stale = NO;
        NSURL *url = [NSURL URLByResolvingBookmarkData:data
            options:NSURLBookmarkResolutionWithoutUI | NSURLBookmarkResolutionWithoutMounting
            relativeToURL:nil bookmarkDataIsStale:&stale error:nil];
        return url.isFileURL ? strdup(url.fileSystemRepresentation) : NULL;
    }
}
