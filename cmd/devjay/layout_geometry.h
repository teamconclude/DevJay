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

// Open inward from the nearest screen edge, centered on the card. Try the
// other sides if necessary; clamp only when no side can fit the whole popup.
static inline DPRect dp_card_popup(DPRect popup, DPRect card, DPRect area) {
    popup = dp_clamp(popup, area);
    double cx = card.x + card.width / 2, cy = card.y + card.height / 2;
    double w = fmax(1, area.width), h = fmax(1, area.height), gap = 10;
    double distance[] = {(card.y-area.y)/h, (area.y+area.height-card.y-card.height)/h,
                         (card.x-area.x)/w, (area.x+area.width-card.x-card.width)/w};
    DPRect choices[] = {{cx-popup.width/2,card.y+card.height+gap,popup.width,popup.height},
                        {cx-popup.width/2,card.y-gap-popup.height,popup.width,popup.height},
                        {card.x+card.width+gap,cy-popup.height/2,popup.width,popup.height},
                        {card.x-gap-popup.width,cy-popup.height/2,popup.width,popup.height}};
    DPRect fallback = popup;
    for (int attempt = 0; attempt < 4; attempt++) {
        int side = 0;
        for (int i = 1; i < 4; i++) if (distance[i] < distance[side]) side = i;
        DPRect candidate = dp_clamp(choices[side], area);
        if (attempt == 0) fallback = candidate;
        // Allow sliding along the edge to stay on screen, but never slide back
        // over the card just because the preferred side has insufficient room.
        if ((side == 0 && candidate.y >= card.y+card.height+gap) ||
            (side == 1 && candidate.y+candidate.height <= card.y-gap) ||
            (side == 2 && candidate.x >= card.x+card.width+gap) ||
            (side == 3 && candidate.x+candidate.width <= card.x-gap)) return candidate;
        distance[side] = INFINITY;
    }
    return fallback;
}

static inline double dp_overlap(DPRect a, DPRect b) {
    return fmax(0, fmin(a.x+a.width,b.x+b.width)-fmax(a.x,b.x)) *
           fmax(0, fmin(a.y+a.height,b.y+b.height)-fmax(a.y,b.y));
}

// Keep the card placement when available. Otherwise try nearby window edges
// and screen edges. On crowded screens prefer distinct title-bar positions,
// then the least overlap, so another window does not disappear under this one.
static inline DPRect dp_avoid_windows(DPRect preferred, DPRect area, const DPRect *occupied, size_t count) {
    preferred = dp_clamp(preferred,area);
    if (!occupied || !count) return preferred;
    count = count < 64 ? count : 64;
    double overlap = 0;
    for (size_t i=0;i<count;i++) overlap += dp_overlap(preferred,occupied[i]);
    if (overlap == 0) return preferred;
    double xs[3+4*64] = {preferred.x,area.x+8,area.x+area.width-preferred.width-8};
    double ys[3+4*64] = {preferred.y,area.y+8,area.y+area.height-preferred.height-8};
    size_t n = 3;
    for (size_t i=0;i<count;i++) {
        DPRect o = occupied[i];
        xs[n]=o.x-preferred.width-10; ys[n++]=o.y-preferred.height-10;
        xs[n]=o.x+o.width+10; ys[n++]=o.y+o.height+10;
        xs[n]=o.x+32; ys[n++]=o.y+o.height-preferred.height-32;
        xs[n]=o.x-32; ys[n++]=o.y+o.height-preferred.height+32;
    }
    DPRect best = preferred;
    double bestOverlap = INFINITY, bestDistance = INFINITY;
    size_t bestAligned = (size_t)-1;
    for (size_t x=0;x<n;x++) for (size_t y=0;y<n;y++) {
        DPRect candidate = dp_clamp((DPRect){xs[x],ys[y],preferred.width,preferred.height},area);
        double covered=0, dx=candidate.x-preferred.x, dy=candidate.y-preferred.y;
        size_t aligned=0;
        for (size_t i=0;i<count;i++) {
            covered += dp_overlap(candidate,occupied[i]);
            if (fabs(candidate.x-occupied[i].x)<24 &&
                fabs(candidate.y+candidate.height-occupied[i].y-occupied[i].height)<24) aligned++;
        }
        double distance = dx*dx+dy*dy;
        // A free spot always wins, even if title bars happen to align across a gap.
        if (covered==0) aligned=0;
        if ((covered==0 && bestOverlap>0) ||
            ((covered==0)==(bestOverlap==0) && (aligned<bestAligned ||
             (aligned==bestAligned && (covered<bestOverlap || (covered==bestOverlap && distance<bestDistance)))))) {
            best=candidate; bestAligned=aligned; bestOverlap=covered; bestDistance=distance;
        }
    }
    return best;
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

// Grow content without moving a bottom-anchored window away from the edge.
// Otherwise preserve the top edge. Never shrink a saved/manual window size.
static inline DPRect dp_grow_height(DPRect frame, DPRect available, double height) {
    if (!isfinite(height) || height <= frame.height || available.height <= 16) return frame;
    double next = fmin(height, available.height - 16);
    if (next <= frame.height) return frame;
    double bottom = frame.y - available.y;
    double top = available.y + available.height - frame.y - frame.height;
    if (!(fabs(bottom) <= available.height * 0.10 && bottom < top)) frame.y -= next - frame.height;
    frame.height = next;
    return dp_clamp(frame, available);
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
