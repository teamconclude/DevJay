export function mountEditorSettings(root, request) {
  const make=(tag,text)=>{const n=document.createElement(tag);if(text)n.textContent=text;return n;};
  root.append(make('h2','Editor'));
  const row=make('div');row.className='editor-settings-row';
  const label=make('label','Preferred editor'),editors=make('select');
  editors.setAttribute('aria-label','Preferred editor');label.append(editors);row.append(label);
  const automaticLabel=make('label');automaticLabel.className='preference editor-settings-auto';
  const automatic=make('input');automatic.type='checkbox';
  automaticLabel.append(automatic,make('span','Automatically use the editor with this project open'));
  const error=make('p');error.className='editor-settings-error error';error.setAttribute('role','alert');error.hidden=true;
  root.append(row,automaticLabel,error);
  let optionsKey='',loading=true;
  const option=(value,text)=>{const n=make('option',text);n.value=value;editors.append(n);};
  const send=data=>{if(loading||editors.disabled)return;editors.disabled=automatic.disabled=true;error.hidden=true;request(data);};
  editors.onchange=()=>send({operation:editors.value==='__choose_app__'?'browse':'select',editor:editors.value==='__choose_app__'?'':editors.value});
  automatic.onchange=()=>send({operation:'automatic',useProject:automatic.checked});
  return {
    renderEditor(data){
      const editor=data.editor;loading=!editor;editors.disabled=automatic.disabled=loading||!!editor?.busy;
      if(!editor)return;
      const key=JSON.stringify([editor.apps,editor.choice]);
      if(key!==optionsKey){
        optionsKey=key;editors.replaceChildren();option('','Choose an editor…');
        for(const app of editor.apps||[])option(app.path,`${app.name}${app.pids?.length?' · running':''}`);
        if(editor.choice&&!(editor.apps||[]).some(a=>a.path===editor.choice))option(editor.choice,'Preferred editor unavailable');
        option('__choose_app__','Choose another application…');
      }
      editors.value=editor.choice||'';automatic.checked=!!editor.useProject;
      error.textContent=editor.error||'';error.hidden=!editor.error;
    }
  };
}
