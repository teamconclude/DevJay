export function mountClaudePermissionSettings(root, request) {
  const make=(tag,text)=>{const n=document.createElement(tag);if(text)n.textContent=text;return n;};
  const section=make('section');
  const title=make('h3','Claude permission prompts');
  const description=make('p','Respond to Claude approval prompts directly from DevJay. Enabling this adds a hook to your global Claude settings.');
  description.className='help';
  const label=make('label');label.className='preference';
  const checkbox=make('input');checkbox.type='checkbox';checkbox.checked=false;checkbox.disabled=true;
  label.append(checkbox,make('span','Enable Claude prompts'));
  const status=make('p','Checking integration…');status.className='help';status.setAttribute('role','status');
  checkbox.onchange=()=>{checkbox.disabled=true;request({operation:checkbox.checked?'enable':'disable'});};
  section.append(title,description,label,status);root.append(section);
  return {render(data){const enabled=!!data.enabled;checkbox.checked=enabled;checkbox.disabled=!data.available;status.textContent=data.error||(data.available?(enabled?'Enabled globally for all sites. New requests stay available for up to 24 hours while DevJay is running. Restart Claude if it does not pick up the hook.':'Enable once for your Claude Code sessions. Existing hooks are preserved; a backup is saved before changes.'):'The local Claude connection is unavailable. Restart DevJay to try again.');}};
}
