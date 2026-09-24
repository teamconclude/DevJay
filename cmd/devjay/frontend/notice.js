// An empty update may only clear a notice from the same source.
export function nextNotice(current, source, value = {}) {
  const text = source === 'snapshot' ? value.quitMessage || value.error || '' : value.text || '';
  if (!text && current && current.source !== source) return current;
  const error = !!text && (source === 'snapshot' ? !value.quitMessage : !!value.error);
  return {source, text, error};
}
