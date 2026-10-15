export function mountProfileSettings(root,scope,request){
  const make=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';if(text)n.textContent=text;return n;};
  const button=(title,fn)=>{const b=make('button','',title);b.type='button';b.onclick=fn;return b;};
  let state={profiles:[],targets:[]},signature='',selection='',current='',editing=null,submitting=false,returnFocus=null;

  const modal=make('div','profile-modal');modal.hidden=true;
  const dialog=make('div','profile-dialog');dialog.tabIndex=-1;dialog.setAttribute('role','dialog');dialog.setAttribute('aria-modal','true');dialog.setAttribute('aria-labelledby','profile-dialog-title');modal.append(dialog);
  const form=make('form'),dialogTitle=make('h2'),description=make('p','help');dialogTitle.id='profile-dialog-title';
  const nameLabel=make('label','profile-field','Name'),name=make('input');name.type='text';name.maxLength=160;name.placeholder='For example, Office';nameLabel.append(name);
  const dialogError=make('p','error');dialogError.setAttribute('role','alert');
  const closeDialog=()=>{
    modal.hidden=true;document.querySelector('#app').inert=false;
    (returnFocus&&document.body.contains(returnFocus)?returnFocus:select).focus();
  };
  const cancel=button('Cancel',closeDialog),submit=make('button');submit.type='submit';
  const dialogActions=make('div','profile-dialog-actions');dialogActions.append(cancel,submit);form.append(dialogTitle,description,nameLabel,dialogError,dialogActions);dialog.append(form);
  const validName=()=>!!name.value.trim();
  name.oninput=()=>{submit.disabled=submitting||(!nameLabel.hidden&&!validName());};
  const openDialog=(operation,profile)=>{
    if(!modal.hidden)return;
    editing={operation,id:profile?.id};dialogError.textContent='';name.value=profile?.name||'';
    const deleting=operation==='delete';nameLabel.hidden=deleting;name.required=!deleting;
    dialogTitle.textContent=operation==='create'?'New profile':deleting?'Delete profile?':'Rename profile';
    description.textContent=operation==='create'?'Starts with your current Sites and preferences, and remembers the connected displays. Window placement is saved separately.':deleting?`Delete “${profile.name}” and its saved window arrangement? Your Sites and folders are kept.`:'';
    submit.textContent=operation==='create'?'Create profile':deleting?'Delete profile':'Rename';submit.className=deleting?'profile-delete-confirm':'';
    submit.disabled=!deleting&&!validName();returnFocus=document.activeElement;modal.hidden=false;document.querySelector('#app').inert=true;
    if(deleting)cancel.focus();else{name.focus();name.select();}
  };
  dialog.addEventListener('keydown',event=>{
    if(event.key==='Escape'){event.preventDefault();event.stopPropagation();if(!submitting)closeDialog();}
    if(event.key==='Tab'){
      const fields=[...(!nameLabel.hidden?[name]:[]),cancel,submit].filter(n=>!n.disabled);
      const index=fields.indexOf(document.activeElement);
      if(!fields.length){event.preventDefault();return;}
      if(event.shiftKey&&index<=0){event.preventDefault();fields.at(-1).focus();}
      else if(!event.shiftKey&&(index<0||index===fields.length-1)){event.preventDefault();fields[0].focus();}
    }
  });
  form.onsubmit=async event=>{
    event.preventDefault();if(submitting||(!nameLabel.hidden&&!validName()))return;
    submitting=true;submit.disabled=true;cancel.disabled=true;name.disabled=true;dialogError.textContent='';dialog.focus();
    try{
      const data={...editing};if(data.operation!=='delete')data.name=name.value.trim();
      if(await request(data))closeDialog();
      else dialogError.textContent=state.message||'Could not save the change. Check the current Profile list before trying again.';
    }catch(error){dialogError.textContent=error?.message||'Could not save the change.';}
    finally{submitting=false;cancel.disabled=false;name.disabled=false;name.oninput();if(!modal.hidden)(nameLabel.hidden?cancel:name).focus();}
  };

  root.append(make('h2','','Profiles'));
  const intro=make('div','profile-intro');const create=button('New profile',()=>openDialog('create'));create.setAttribute('aria-haspopup','dialog');
  intro.append(make('p','help','Save a working arrangement for Office, Home, Laptop, or any monitor combination.'),create);root.append(intro);
  const unknown=make('div','profile-notice');unknown.hidden=true;root.append(unknown);
  const label=make('label','profile-field','Active profile'),select=make('select');select.setAttribute('aria-label','Active profile');label.append(select);
  const use=button('Use with these displays',()=>request({operation:'use',id:select.value}));
  const choose=make('div','profile-row');choose.append(label,use);root.append(choose);
  select.onchange=()=>{selection=select.value;};
  root.append(make('h3','','Connected displays'));
  const displays=make('div','profile-displays');root.append(displays);
  const autoLabel=make('label','preference'),automatic=make('input');automatic.type='checkbox';automatic.onchange=()=>request({operation:'automatic',enabled:automatic.checked});autoLabel.append(automatic,document.createTextNode('Switch Profiles automatically when displays change'));root.append(autoLabel);
  root.append(make('p','help','Each Profile remembers Site desktops and window positions. Temporary monitor disconnections never replace a saved arrangement.'));
  root.append(make('h3','','Manage profiles'));
  const managed=make('ul','profile-list');managed.setAttribute('aria-label','Manage profiles');root.append(managed);
  root.append(make('p','help','Switch to another Profile before deleting the active one.'));
  const error=make('p','error');error.setAttribute('role','status');root.append(error);document.body.append(modal);
  const details=make('details','profile-scope-picker'),summary=make('summary'),choices=make('div','profile-scope-options');details.append(summary,choices);scope.append(details);
  const sendTargets=targets=>request({operation:'scope',targets});
  return {render(next){
    if(!next)return;state=next;state.profiles||=[];state.targets||=[];
    unknown.hidden=!state.unknown;unknown.textContent='This monitor combination has no active Profile. Create one or choose a Profile below. Saved arrangements are protected until you choose.';
    automatic.checked=!!state.automatic;error.textContent=state.message||'';
    create.disabled=!state.current;select.disabled=!state.current;use.disabled=!state.current;
    const key=JSON.stringify([state.current,state.profiles.map(p=>[p.id,p.name]),state.displays,state.targets]);
    if(key===signature)return;const previous=signature;signature=key;
    if(current!==state.current||!state.profiles.some(p=>p.id===selection)||!previous)selection=state.current;current=state.current;
    select.replaceChildren();for(const p of state.profiles){const option=make('option','',p.name);option.value=p.id;select.append(option);}select.value=selection;
    managed.replaceChildren();for(const p of state.profiles){
      const row=make('li','profile-list-row'),title=make('span','profile-list-name',p.name);
      const rename=button('Rename',()=>openDialog('rename',p));rename.setAttribute('aria-label',`Rename ${p.name}`);rename.setAttribute('aria-haspopup','dialog');
      const remove=button('Delete',()=>openDialog('delete',p));remove.setAttribute('aria-label',`Delete ${p.name}`);remove.setAttribute('aria-haspopup','dialog');remove.disabled=p.id===state.current;remove.title=remove.disabled?'Switch to another Profile before deleting this one.':'';
      row.append(title,rename,remove);managed.append(row);
    }
    displays.replaceChildren();for(const display of [...(state.displays||[])].sort((a,b)=>a.X-b.X)){const tile=make('div','profile-display');tile.append(make('div','profile-screen','▱'),make('strong','',display.name));displays.append(tile);}
    const targets=state.profiles.filter(p=>state.targets.includes(p.id));
    summary.textContent=`Apply changes to: ${targets.length===state.profiles.length&&targets.length>1?'All Profiles':targets.map(p=>p.name).join(',')||'Current Profile'}`;
    choices.replaceChildren();const shortcuts=make('div','profile-row');shortcuts.append(button('This Profile',()=>sendTargets([state.current])),button('All Profiles',()=>sendTargets(state.profiles.map(p=>p.id))));choices.append(shortcuts);
    for(const p of state.profiles){const label=make('label','preference'),box=make('input');box.type='checkbox';box.checked=state.targets.includes(p.id);box.disabled=p.id===state.current;box.onchange=()=>sendTargets(box.checked?[...state.targets,p.id]:state.targets.filter(id=>id!==p.id));label.append(box,document.createTextNode(p.name));choices.append(label);}
    choices.append(make('p','help','Only settings you change are applied to the selected Profiles. Window positions stay separate.'));
  }};
}
