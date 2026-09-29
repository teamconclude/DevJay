export function prCheckIndicator(pr) {
  switch (pr.ciState) {
    case 'failure': return {symbol:'×', tone:'ci-failed', label:'CI failed'};
    case 'pending': return {symbol:'◷', tone:'', label:'CI running or queued'};
    case 'action_required': return {symbol:'!', tone:'ci-failed', label:'CI needs attention'};
    case 'cancelled': return {symbol:'⊘', tone:'', label:'CI cancelled'};
    case 'neutral': return {symbol:'', tone:'', label:'CI checks skipped or neutral'};
    case 'success': return {symbol:'', tone:'', label:'CI passed'};
    case 'none': return {symbol:'', tone:'', label:'No CI checks reported'};
    default: return {symbol:'', tone:'', label:pr.state === 'Merged' ? '' : 'CI status unavailable'};
  }
}
export function prTagTone(pr) {
  if (['failure','action_required'].includes(pr.ciState)) return 'ci-failed';
  return pr.state === 'Approved' && !pr.isDraft ? 'pr-approved' : '';
}
export function prDescription(pr) {
  return [`#${pr.number}`, pr.isDraft ? 'Draft' : pr.state, prCheckIndicator(pr).label].filter(Boolean).join(' · ');
}
