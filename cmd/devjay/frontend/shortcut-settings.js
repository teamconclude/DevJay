export function mountShortcutSettings(root, request) {
  const make=(tag,text,cls)=>{const n=document.createElement(tag);if(text)n.textContent=text;if(cls)n.className=cls;return n;};
  root.append(make('h2','Keyboard shortcuts'),make('p','These shortcuts work in every app and are shared across Profiles. Click Record, then press a key with Control, Option or Command. Changes are saved automatically.','help'));
  const message=make('p','','shortcut-message help');message.hidden=true;message.setAttribute('role','status');message.setAttribute('aria-live','polite');
  const list=make('ul','','shortcut-list');list.setAttribute('aria-label','Global keyboard shortcuts');
  const cancel=make('button','Cancel recording','shortcut-cancel');cancel.type='button';cancel.hidden=true;
  root.append(message,cancel,list);
  const controls=make('div','','shortcut-actions'),restore=make('button','Restore defaults'),retry=make('button','Retry unavailable shortcuts');
  restore.type=retry.type='button';controls.append(restore,retry);root.append(controls);
  root.append(make('p','Site numbers follow the card order. Hold a Site shortcut’s modifiers to preview its key on visible cards. Escape or a click cancels recording; a hidden strip stays hidden.','help'));
  root.append(make('h3','Menu shortcuts'));
  const menu=make('ul','','shortcut-menu-list');
  for(const [key,title] of [['⌘,','Open Settings'],['⌘0','Restore full strip'],['⌘1–9','Go to a Site'],['⌘Q','Quit DevJay']]) {
    const item=make('li');item.append(make('span',title),make('kbd',key));menu.append(item);
  }
  root.append(menu,make('p','Menu shortcuts apply while DevJay has keyboard focus and keep their standard bindings.','help'));
  let state={items:[]},pending=false;
  const rows=new Map();
  const send=async data=>{
    if(pending)return;pending=true;render(state);
    try{await request(data);}finally{pending=false;render(state);}
  };
  cancel.onclick=()=>send({operation:'cancel'});
  restore.onclick=()=>send({operation:'reset-all'});
  retry.onclick=()=>send({operation:'retry'});
  function render(value) {
    state=value||{items:[]};const items=state.items||[],recording=state.recording||'';
    message.textContent=recording?'Press the shortcut, then release its key. Escape or a click cancels.':state.message||'';
    cancel.hidden=!recording;cancel.disabled=pending;
    message.hidden=!message.textContent;message.classList.toggle('error',!!state.message&&!recording);
    const ids=new Set(items.map(item=>item.id));for(const [id,row] of rows){if(!ids.has(id)){row.root.remove();rows.delete(id);}}
    for(const item of items) {
      let row=rows.get(item.id);
      if(!row) {
        const entry=make('li','','shortcut-row'),text=make('div','','shortcut-description'),title=make('span'),status=make('small'),key=make('kbd'),buttons=make('div','','shortcut-buttons'),record=make('button','Record'),reset=make('button','Reset');
        record.type=reset.type='button';entry.dataset.shortcutId=item.id;text.append(title,status);buttons.append(record,reset);entry.append(text,key,buttons);list.append(entry);
        record.onclick=()=>send({operation:'record',id:item.id});reset.onclick=()=>send({operation:'reset',id:item.id});
        row={root:entry,title,status,key,record,reset};rows.set(item.id,row);
      }
      row.title.textContent=item.title;row.key.textContent=recording===item.id?'Listening…':item.label;
      row.status.textContent=item.unavailable?'Unavailable — choose another combination':item.custom?`Default: ${item.default}`:'';
      row.status.hidden=!row.status.textContent;row.status.classList.toggle('error',!!item.unavailable);
      row.record.setAttribute('aria-label',`Record shortcut for ${item.title}`);
      row.reset.setAttribute('aria-label',`Reset shortcut for ${item.title}`);row.reset.title=`Restore ${item.default}`;
      row.record.disabled=pending||!!recording;row.reset.disabled=pending||!!recording||!item.custom;
      row.root.classList.toggle('recording',recording===item.id);
    }
    restore.disabled=pending||!!recording||!items.some(item=>item.custom);
    retry.hidden=!items.some(item=>item.unavailable);retry.disabled=pending||!!recording;
  }
  return {render};
}
