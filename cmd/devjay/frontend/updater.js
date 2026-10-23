export function updatePresentation(update = {}, settings = false) {
  const version = update.version || '';
  switch (update.state) {
    case 'available': return {text:`Update to ${version}`, operation:'download'};
    case 'downloading': return {text:`Downloading ${update.percent || 0}%`, disabled:true};
    case 'ready': return {text:'Restart to update', operation:'install', primary:true};
    case 'installing': return {text:'Restarting…', disabled:true};
    case 'manual': return {text:`Download ${version}`, operation:'manual'};
    case 'error': return {text:'Update failed · Retry', operation:'check'};
    case 'checking': return {text:'Checking for updates…', disabled:true, hidden:!settings};
    case 'disabled': return {text:'Development build', disabled:true, hidden:!settings};
    default: return {text:'Check for updates', operation:'check', hidden:!settings};
  }
}
