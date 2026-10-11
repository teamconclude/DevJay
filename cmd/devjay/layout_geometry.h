#ifndef DEVJAY_LAYOUT_GEOMETRY_H
#define DEVJAY_LAYOUT_GEOMETRY_H
#include <math.h>
#include <stddef.h>

typedef struct { double x, y, width, height; } DPRect;

// Floating windows may intentionally overlap the Dock's reserved strip.
// Keep the menu bar excluded, but anchor to the physical display's other edges.
static inline DPRect dp_placement_bounds(DPRect screen, DPRect visible) {
    double top = fmin(screen.y + screen.height, visible.y + visible.height);
    screen.height = top - screen.y;
    return screen;
}

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

// Overlay the owning window. A control anchor moves only the horizontal
// position, keeping the dialog vertically centered over its parent.
static inline DPRect dp_parent_popup(DPRect popup, DPRect parent, DPRect control, DPRect area) {
    popup = dp_clamp(popup, area);
    popup.x = control.width > 0 ? control.x + control.width + 10 : parent.x + (parent.width-popup.width)/2;
    popup.y = parent.y + (parent.height-popup.height)/2;
    return dp_clamp(popup, area);
}

// Keep card-launched windows horizontally aligned with the clicked card.
// Prefer above; use below when above cannot fit, then clamp to the display.
static inline DPRect dp_card_popup(DPRect popup, DPRect card, DPRect area) {
    popup = dp_clamp(popup, area);
    popup.x = card.x + (card.width-popup.width)/2;
    popup.y = card.y + card.height + 10;
    double below = card.y - popup.height - 10;
    if (popup.y + popup.height > area.y + area.height - 8 && below >= area.y + 8) popup.y = below;
    return dp_clamp(popup, area);
}

// AppKit coordinates grow upward. Prefer the left/top offsets unless a window
// sits within 10% of the usable width/height of the nearer right/bottom edge.
// A negative gap still counts as near the edge; the final clamp brings an
// off-screen window back inside the display.
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

// Fit content without moving a bottom-anchored window away from the edge.
// Otherwise preserve the top edge, including when removing unused height.
static inline DPRect dp_fit_height(DPRect frame, DPRect available, double height) {
    if (!isfinite(height) || height <= 0 || available.height <= 16) return frame;
    double next = fmin(height, available.height - 16);
    if (next == frame.height) return frame;
    double bottom = frame.y - available.y;
    double top = available.y + available.height - frame.y - frame.height;
    if (!(fabs(bottom) <= available.height * 0.10 && bottom < top)) frame.y -= next - frame.height;
    frame.height = next;
    return dp_clamp(frame, available);
}

// Ordinary status updates preserve a saved/manual height and only grow.
static inline DPRect dp_grow_height(DPRect frame, DPRect available, double height) {
    if (height <= frame.height || available.height - 16 <= frame.height) return frame;
    return dp_fit_height(frame, available, height);
}

// Initial panel estimate (216x152 cards, 8px gaps, and content padding).
// The frontend subsequently fits actual content height. Modes: row, column,
// and wrapping tiles. Saved free placements retain their full extent.
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
