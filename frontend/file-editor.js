// The backend resolves the selected file and editor; no client-supplied path is opened.
export function mountFileEditor(root, request) {
  const open=document.createElement('button');open.type='button';open.textContent='Open in editor';
  const message=document.createElement('span');message.className='file-editor-message';message.setAttribute('role','status');
  root.append(open,message);
  let state=null;
  open.onclick=()=>{
    if(open.disabled||!state)return;
    open.disabled=true;request({version:state.version,operation:'file-open'});
  };
  return v=>{
    state=v;const editor=v.editor;
    open.textContent=editor?.busy?'Opening…':editor?.resolved?`Open in ${editor.resolved.name}`:'Open in editor';
    open.disabled=!!v.loading||!editor?.canOpen||!!editor?.busy;
    open.title=[`Open the current working-tree file, including when viewing an older commit.`,editor?.note].filter(Boolean).join('\n');
    message.textContent=editor?.error||(!editor?.canOpen?editor?.note:'')||'';
    message.hidden=!message.textContent;message.className=`file-editor-message ${editor?.error?'error':'muted'}`;
  };
}
