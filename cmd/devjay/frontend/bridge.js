// Action events need an application acknowledgement, not just delivery to Wails.
export function createBridge(send, report, timeout = 10000) {
  const pending = new Map();
  const documentID = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random()}`;
  let sequence = 0;
  function reply({requestID, error}) {
    pending.get(requestID)?.(error);
  }
  async function emit(name, data) {
    let timer, requestID;
    try {
      const acknowledged = name === 'action-prepare' || name === 'action-confirm' || name === 'branches-open' ||
        (name === 'profile' && ['create','rename','delete'].includes(data?.operation));
      const completion = new Promise((resolve, reject) => {
        timer = setTimeout(() => reject(new Error('The request timed out. Check the action’s current state before trying again.')), timeout);
        if (acknowledged) {
          requestID = `${documentID}-${++sequence}`;
          pending.set(requestID, error => error ? reject(new Error(error)) : resolve());
        }
        Promise.resolve().then(() => send(`devjay:${name}`, acknowledged ?
          (name === 'action-confirm' ? {token:data, requestID} : {...data, requestID}) : data))
          .then(() => { if (!acknowledged) resolve(); }, reject);
      });
      await completion;
      report('', false);
      return true;
    } catch (error) {
      report(error?.message || 'Could not complete the request. Try again.', true);
      return false;
    } finally {
      clearTimeout(timer);
      if (requestID) pending.delete(requestID);
    }
  }
  return {emit, reply};
}
