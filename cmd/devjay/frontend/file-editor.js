// The backend resolves the selected file and editor; no client-supplied path is opened.
export function mountFileEditor(root, request) {
  const open=document.createElement('button');open.type='button';open.textContent='Open in editor';
  const message=document.createElement('span');message.className='file-editor-message';message.setAttribute('role','status');
  root.append(open,message);
  let state=null, displayedEditor=null;
  open.onclick=()=>{
    if(open.disabled||!state)return;
    open.disabled=true;request({version:state.version,operation:'file-open'});
  };
  return v=>{
    if(state?.siteID!==v.siteID)displayedEditor=null;
    state=v;
    // Editor discovery follows each diff read. Preserve the label and its
    // geometry while waiting, but never enable an unverified file-open action.
    if(v.editor)displayedEditor=v.editor;
    const editor=v.editor||displayedEditor;
    open.textContent=editor?.busy?'Opening…':editor?.resolved?`Open in ${editor.resolved.name}`:'Open in editor';
    open.disabled=!!v.loading||!v.editor?.canOpen||!!v.editor?.busy;
    open.className=(v.loading||!v.editor)&&editor?.canOpen&&!editor?.busy?'editor-pending':'';
    open.title=[`Open the current working-tree file, including when viewing an older commit.`,editor?.note].filter(Boolean).join('\n');
    message.textContent=editor?.error||(!editor?.canOpen?editor?.note:'')||'';
    message.hidden=!message.textContent;message.className=`file-editor-message ${editor?.error?'error':'muted'}`;
  };
}
