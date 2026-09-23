// One identity per document, independent of the native window. A previous
// document must never acknowledge a replacement that has not rendered yet.
export function uiReady(send, documentID = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random()}`) {
  let sent = false, pending = false;
  return async function rendered() {
    if (sent || pending) return;
    pending = true;
    try { await send(documentID); sent = true; }
    catch { /* The next rendered snapshot can retry a failed bridge call. */ }
    finally { pending = false; }
  };
}
