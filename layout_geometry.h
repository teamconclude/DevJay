#ifndef DEVJAY_LAYOUT_GEOMETRY_H
#define DEVJAY_LAYOUT_GEOMETRY_H
#include <math.h>
#include <stddef.h>

typedef struct { double x, y, width, height; } DPRect;

static inline DPRect dp_clamp(DPRect frame, DPRect visible) {
    double inset = 8;
    if (visible.width <= 2 * inset || visible.height <= 2 * inset) inset = 0;
    visible.x += inset; visible.y += inset;
    visible.width -= 2 * inset; visible.height -= 2 * inset;
    frame.width = fmin(frame.width, visible.width);
    frame.height = fmin(frame.height, visible.height);
    frame.x = fmax(visible.x, fmin(frame.x, visible.x + visible.width - frame.width));
    frame.y = fmax(visible.y, fmin(frame.y, visible.y + visible.height - frame.height));
    return frame;
}

// AppKit coordinates grow upward. Prefer the left/top offsets unless a window
// sits within 10% of the usable width/height of the nearer right/bottom edge.
// A negative gap can occur when a user places it over the Dock reserved area;
// that still counts as near the edge, and the final clamp keeps it usable.
static inline DPRect dp_reanchor(DPRect frame, DPRect old, DPRect next) {
    if (old.width <= 0 || old.height <= 0) return dp_clamp(frame, next);
    double left = frame.x - old.x, right = old.x + old.width - frame.x - frame.width;
    double bottom = frame.y - old.y, top = old.y + old.height - frame.y - frame.height;
    frame.x = fabs(right) <= old.width * 0.10 && right < left
        ? next.x + next.width - right - frame.width : next.x + left;
    frame.y = fabs(bottom) <= old.height * 0.10 && bottom < top
        ? next.y + bottom : next.y + next.height - top - frame.height;
    return dp_clamp(frame, next);
}

// Match panel cards (216x152), 8px gaps, and content padding. Modes: row,
// column, and wrapping tiles. Saved free placements retain their full extent.
static inline DPRect dp_panel_size(int mode, size_t count, double maxWidth, double maxHeight,
                                   double header, double extentWidth, double extentHeight) {
    double n = fmax(1, (double)count), columns = n;
    if (mode == 1) columns = 1;
    if (mode == 2) {
        columns = ceil(sqrt(n));
        columns = fmax(1, fmin(columns, floor((maxWidth - 10) / 224)));
    }
    double rows = mode == 0 ? 1 : ceil(n / columns);
    double width = columns * 224 + 10;
    double height = rows * 160 + 9 + header;
    if (mode == 2 && extentWidth > 0) {
        width = extentWidth + 18;
        height = extentHeight + 17 + header;
    }
    return (DPRect){0, 0, fmin(maxWidth, fmax(234, width)), fmin(maxHeight, fmax(174, height))};
}

// Prefer the display containing most of the window; if none overlaps, choose
// the nearest display edge. Negative desktop coordinates are valid.
static inline size_t dp_nearest_screen(DPRect frame, const DPRect *screens, size_t count) {
    size_t best = 0;
    double bestArea = -1, bestDistance = INFINITY;
    for (size_t i = 0; i < count; i++) {
        DPRect s = screens[i];
        double w = fmax(0, fmin(frame.x + frame.width, s.x + s.width) - fmax(frame.x, s.x));
        double h = fmax(0, fmin(frame.y + frame.height, s.y + s.height) - fmax(frame.y, s.y));
        double dx = fmax(0, fmax(s.x - frame.x - frame.width, frame.x - s.x - s.width));
        double dy = fmax(0, fmax(s.y - frame.y - frame.height, frame.y - s.y - s.height));
        double distance = dx * dx + dy * dy;
        if (w * h > bestArea || (w * h == bestArea && distance < bestDistance)) {
            best = i; bestArea = w * h; bestDistance = distance;
        }
    }
    return best;
}
#endif
