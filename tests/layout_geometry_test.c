#include "../cmd/devjay/layout_geometry.h"
#include <assert.h>
#include <stdio.h>
static void check(DPRect a, DPRect b) {
    assert(a.x == b.x && a.y == b.y && a.width == b.width && a.height == b.height);
}
int main(void) {
    DPRect screen = {0, 50, 1440, 830}; // Dock/menu excluded.
    check(dp_clamp((DPRect){100,100,560,156}, screen), (DPRect){100,100,560,156});
    check(dp_clamp((DPRect){1300,800,560,156}, screen), (DPRect){872,716,560,156});
    check(dp_clamp((DPRect){-300,-100,300,245}, screen), (DPRect){8,58,300,245});
    check(dp_clamp((DPRect){100,100,560,156}, (DPRect){0,0,400,120}), (DPRect){8,8,384,104});
    check(dp_clamp((DPRect){-1500,100,300,245}, (DPRect){-1920,0,1920,1080}), (DPRect){-1500,100,300,245});
    DPRect screens[] = {{-1920,0,1920,1080}, {0,0,1440,900}, {1600,0,1920,1080}};
    assert(dp_nearest_screen((DPRect){-1800,50,300,245}, screens,3) == 0);
    assert(dp_nearest_screen((DPRect){1450,100,100,100}, screens,3) == 1); // gap
    assert(dp_nearest_screen((DPRect){4000,100,300,245}, screens,3) == 2); // unplugged far right
    assert(dp_nearest_screen((DPRect){1400,100,400,245}, screens,3) == 2); // most overlap
    check(dp_panel_size(0, 5, 2000, 1200, 0, 0, 0), (DPRect){0,0,1130,174});
    check(dp_panel_size(1, 5, 2000, 1200, 0, 0, 0), (DPRect){0,0,234,809});
    check(dp_panel_size(2, 5, 2000, 1200, 0, 0, 0), (DPRect){0,0,682,329});
    check(dp_panel_size(2, 8, 500, 1200, 22, 0, 0), (DPRect){0,0,458,671});
    check(dp_panel_size(2, 5, 2000, 1200, 0, 800, 450), (DPRect){0,0,818,467});
    check(dp_panel_size(1, 20, 1000, 700, 0, 0, 0), (DPRect){0,0,234,700});
    check(dp_panel_size(0, 0, 2000, 1200, 0, 0, 0), (DPRect){0,0,234,174});
    DPRect old = {0,0,1440,900}, larger = {0,0,1920,1080};
    check(dp_reanchor((DPRect){1120,30,300,200}, old, larger), (DPRect){1600,30,300,200});
    check(dp_reanchor((DPRect){1120,670,300,200}, old, larger), (DPRect){1600,850,300,200});
    check(dp_reanchor((DPRect){400,300,300,200}, old, larger), (DPRect){400,480,300,200});
    check(dp_reanchor((DPRect){1120,30,300,200}, old, (DPRect){-1280,100,1280,720}), (DPRect){-320,130,300,200});
    check(dp_reanchor((DPRect){1120,30,300,200}, old, (DPRect){0,0,800,600}), (DPRect){480,30,300,200});
    check(dp_reanchor((DPRect){1120,30,300,200}, old, (DPRect){0,0,240,160}), (DPRect){8,8,224,144});
    check(dp_reanchor((DPRect){1120,80,300,200}, (DPRect){0,50,1440,830}, (DPRect){0,70,1920,990}), (DPRect){1600,100,300,200});
    // A user-positioned Site can overlap the Dock's reserved area. It must
    // remain bottom-anchored rather than acquiring a large top-based offset.
    check(dp_reanchor((DPRect){2877,42,289,173}, (DPRect){0,90,3200,1680}, (DPRect){0,90,3840,2040}), (DPRect){3517,98,289,173});
    check(dp_reanchor((DPRect){-310, -478, 300,174}, (DPRect){-2560,-460,2560,1410}, (DPRect){-3840,-508,3840,2130}), (DPRect){-310,-500,300,174});
    check(dp_reanchor((DPRect){1160,30,300,200}, old, larger), (DPRect){1612,30,300,200});
    // Thresholds use the old usable screen dimensions; anchored gaps remain
    // in points rather than growing with the new resolution.
    check(dp_reanchor((DPRect){1500,150,300,200}, (DPRect){0,0,2000,1500}, (DPRect){0,0,3000,2000}), (DPRect){2500,150,300,200});
    check(dp_reanchor((DPRect){1499,151,300,200}, (DPRect){0,0,2000,1500}, (DPRect){0,0,3000,2000}), (DPRect){1499,651,300,200});
    check(dp_reanchor((DPRect){600,60,300,200}, (DPRect){0,0,1000,600}, (DPRect){0,0,1500,900}), (DPRect){1100,60,300,200});
    check(dp_reanchor((DPRect){599,61,300,200}, (DPRect){0,0,1000,600}, (DPRect){0,0,1500,900}), (DPRect){599,361,300,200});
    // Restoration must preserve a bottom gap even when the Dock reserves 90pt.
    DPRect display = {0,0,3200,1800};
    DPRect withDock = dp_placement_bounds(display, (DPRect){0,90,3200,1680});
    DPRect hiddenDock = dp_placement_bounds(display, (DPRect){0,0,3200,1770});
    check(withDock, (DPRect){0,0,3200,1770});
    check(withDock, hiddenDock);
    check(dp_reanchor((DPRect){16,10,289,173}, withDock, withDock), (DPRect){16,10,289,173});
    check(dp_reanchor((DPRect){16,10,289,173}, hiddenDock, withDock), (DPRect){16,10,289,173});
    DPRect bigger = dp_placement_bounds((DPRect){0,0,3840,2160}, (DPRect){0,90,3840,2040});
    check(dp_reanchor((DPRect){2895,10,289,173}, withDock, bigger), (DPRect){3535,10,289,173});
    // Side Docks also leave placement bounds unchanged. Menu bar stays excluded.
    check(dp_placement_bounds(display, (DPRect){90,0,3110,1770}), withDock);
    check(dp_placement_bounds(display, (DPRect){0,0,3110,1770}), withDock);
    check(dp_clamp((DPRect){16,1750,289,173}, withDock), (DPRect){16,1589,289,173});
    DPRect negative = dp_placement_bounds((DPRect){-3840,-508,3840,2160}, (DPRect){-3840,-418,3840,2040});
    check(dp_reanchor((DPRect){2895,10,289,173}, withDock, negative), (DPRect){-305,-498,289,173});
    check(dp_clamp((DPRect){16,-100,289,173}, withDock), (DPRect){16,8,289,173});
    check(dp_grow_height((DPRect){100,10,300,174}, old, 260), (DPRect){100,10,300,260});
    check(dp_grow_height((DPRect){100,600,300,174}, old, 260), (DPRect){100,514,300,260});
    check(dp_grow_height((DPRect){100,10,300,260}, old, 174), (DPRect){100,10,300,260});
    check(dp_grow_height((DPRect){100,10,300,174}, old, 2000), (DPRect){100,8,300,884});
    check(dp_grow_height((DPRect){-310,-498,300,174}, negative, 260), (DPRect){-310,-498,300,260});
    check(dp_grow_height((DPRect){100,10,300,174}, old, NAN), (DPRect){100,10,300,174});
    DPRect popup = {0,0,400,300}, area = {0,0,1200,900};
    check(dp_card_popup(popup,(DPRect){500,10,200,140},area),(DPRect){400,160,400,300});
    check(dp_card_popup(popup,(DPRect){500,750,200,140},area),(DPRect){400,440,400,300});
    check(dp_card_popup(popup,(DPRect){10,380,200,140},area),(DPRect){220,300,400,300});
    check(dp_card_popup(popup,(DPRect){990,380,200,140},area),(DPRect){580,300,400,300});
    check(dp_card_popup(popup,(DPRect){500,10,200,140},(DPRect){0,80,1200,800}),(DPRect){400,160,400,300});
    check(dp_card_popup(popup,(DPRect){-1190,-50,200,140},(DPRect){-1200,-430,1200,900}),(DPRect){-980,-130,400,300});
    DPRect small = dp_card_popup(popup,(DPRect){100,100,100,50},(DPRect){0,0,300,220});
    assert(small.width == 284 && small.height == 204 && small.x == 8 && small.y == 8);
    DPRect preferred = {400,160,400,300}, occupied[] = {{400,160,400,300},{400,470,400,300}};
    check(dp_avoid_windows(preferred,area,NULL,0),preferred);
    check(dp_avoid_windows(preferred,area,(DPRect[]){{8,8,200,140}},1),preferred);
    DPRect placed = dp_avoid_windows(preferred,area,occupied,1);
    check(placed,(DPRect){400,470,400,300});
    placed = dp_avoid_windows(preferred,(DPRect){0,0,1600,1000},occupied,2);
    assert(dp_overlap(placed,occupied[0]) == 0 && dp_overlap(placed,occupied[1]) == 0);
    assert(placed.x >= 8 && placed.x+placed.width <= 1592 && placed.y >= 8 && placed.y+placed.height <= 992);
    // A crowded desktop still exposes a distinct title bar; no resizing to
    // tiny windows and no dependence on positive display coordinates.
    DPRect large = {100,100,900,700};
    placed = dp_avoid_windows(large,area,&large,1);
    assert(fabs(placed.x-large.x)>=24 || fabs(placed.y-large.y)>=24);
    assert(placed.width==large.width && placed.height==large.height);
    DPRect shifted = {preferred.x-1600,preferred.y-400,preferred.width,preferred.height};
    placed = dp_avoid_windows(shifted,(DPRect){-1600,-400,1200,900},&shifted,1);
    check(placed,(DPRect){-1200,70,400,300});
    check(dp_avoid_windows((DPRect){8,8,284,204},(DPRect){0,0,300,220},(DPRect[]){{8,8,284,204}},1),(DPRect){8,8,284,204});
    puts("Window geometry: 62 checks passed");
}
