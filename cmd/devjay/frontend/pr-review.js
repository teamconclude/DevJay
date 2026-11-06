// Comments decorate the existing Review. GitHub data never supplies its file
// list, diff presentation, selection, or navigation.
export function createPRReview(root, request) {
  const el = (tag, cls = '', text = '') => { const n = document.createElement(tag); n.className = cls; if (text) n.textContent = text; return n; };
  const button = (text, label, action, cls = '') => { const n = el('button', cls, text); n.type = 'button'; n.setAttribute('aria-label', label); n.onclick = action; return n; };
  const input = (tag, label, cls) => { const n = el(tag, cls); n.setAttribute('aria-label', label); return n; };
  const uuid = () => globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`;
  let pendingReview = null;
  let view = null, loadID = 0, snapshot = null, loading = false, busy = false, identity = '', loadFailed = false;
  let draft = null, storageKey = '', revisionKey = '', storedRevision = '', conflict = false, storageError = '', error = '', success = '', draftGuard = false;
  let target = null, anchorID = 0, anchorPending = false, rangeStart = null, pendingClick = null, restoreFocus = false;
  const frame = root.querySelector('.branch-review'); frame.classList.add('br-with-comments');
  const controls = el('div', 'pr-review-actions'); controls.hidden = true;
  const choosePR = input('select', 'Pull request for review comments', 'pr-select');
  const finish = button('Review comments', 'Finish PR review', () => { panel.hidden = false; renderDrafts(); panel.focus(); }, 'pr-primary');
  controls.append(choosePR, finish); frame.querySelector('.br-footer').append(controls);
  const notice = el('div', 'pr-notice'); notice.setAttribute('role', 'status'); notice.hidden = true; frame.append(notice);
  const composer = el('section', 'pr-composer pr-review-card'); composer.hidden = true; composer.setAttribute('aria-label', 'Inline PR review comment');
  const location = el('header', 'pr-comment-header'); const composerStatus = el('p', 'pr-muted');
  const comment = input('textarea', 'Review comment (Markdown supported)', 'pr-comment-text'); comment.rows = 4; comment.maxLength = 60000;
  const saveComment = button('Add review comment', 'Save inline comment to draft review', saveInline, 'pr-primary');
  const cancelComment = button('Cancel', 'Cancel inline comment', () => { if (draft) { draft.composer = null; persist(); } pendingClick = null; hideComposer(); updateControls(); });
  const composerContent = el('div', 'pr-comment-content'); composerContent.append(composerStatus, comment);
  const composerActions = el('footer', 'pr-comment-actions'); composerActions.append(cancelComment, saveComment);
  composer.append(location, composerContent, composerActions);
  const panel = el('section', 'pr-submit-panel'); panel.hidden = true; panel.tabIndex = -1; panel.setAttribute('aria-label', 'PR review comments');
  const heading = el('div', 'pr-panel-title'); heading.append(el('h2', '', 'Review comments'), button('×', 'Close review summary', () => { panel.hidden = true; }));
  const account = el('p', 'pr-muted'); const draftList = el('div', 'pr-draft-list');
  const summary = input('textarea', 'Review summary (Markdown supported)', 'pr-summary'); summary.rows = 4; summary.maxLength = 60000; summary.placeholder = 'Leave an overall review comment…';
  const action = input('select', 'Review action', 'pr-action');
  for (const [value, label] of [['COMMENT', 'Comment'], ['APPROVE', 'Approve'], ['REQUEST_CHANGES', 'Request changes']]) { const option = el('option', '', label); option.value = value; action.append(option); }
  const summaryLabel = el('label', 'pr-summary-label', 'Summary (optional)'); summaryLabel.append(summary);
  const publish = button('Submit review', 'Submit review to GitHub', submit, 'pr-primary');
  const retryLabel = el('label', 'pr-retry'); const retryConfirmed = input('input', 'I checked GitHub and this review was not posted', ''); retryConfirmed.type = 'checkbox';
  retryLabel.append(retryConfirmed, el('span', '', 'I checked GitHub and this review was not posted.'));
  const retrySubmission = button('Retry submission', 'Retry review submission after checking GitHub', () => submit(true));
  const sharedReload = button('Reload saved draft', 'Reload draft changed in another window', () => { restoreDraft(); renderDrafts(); updateControls(); }); sharedReload.hidden = true;
  const reload = button('Refresh PR details', 'Refresh PR comment details', () => load());
  const github = el('a', '', 'Open PR on GitHub'); github.target = '_blank'; github.rel = 'noreferrer';
  const earlier = el('details', 'pr-earlier'); earlier.hidden = true;
  const commentRecovery = el('div', 'pr-comment-recovery');
  const checkComment = button('Check comment status', 'Check pending comment status', () => sendComment(true));
  const retryCommentLabel = el('label', 'pr-retry'); const retryCommentConfirmed = input('input', 'I checked GitHub and the comment change was not saved', ''); retryCommentConfirmed.type = 'checkbox';
  retryCommentLabel.append(retryCommentConfirmed, el('span', '', 'I checked GitHub and this change was not saved.'));
  const retryComment = button('Retry comment change', 'Retry pending comment change', () => { if(retryCommentConfirmed.checked)sendComment(false,true); });
  commentRecovery.append(checkComment,retryCommentLabel,retryComment);
  panel.append(heading, account, draftList, commentRecovery, summaryLabel, action, publish, retryLabel, retrySubmission, sharedReload, earlier, el('hr'), reload, github);
  (frame.querySelector('.br-body') || frame).append(panel);
  function readStore() { try { return JSON.parse(localStorage.getItem(storageKey) || '{}'); } catch { return {}; } }
  function guardDraft(value) {
    if (!snapshot || value === draftGuard) return;
    draftGuard = value;
    requestPR('pr-review-draft', {token:snapshot.token, editing:value}).catch(() => {});
  }
  function emptyDraft() { return {requestID:uuid(), body:'', event:'COMMENT', comments:[], composer:null, pending:false, locations:{}, commentWrite:null, revision:uuid()}; }
  function persist() {
    if (!storageKey || !draft) return false;
    const store = readStore(), current = store[revisionKey];
    if (current && current.revision !== storedRevision) {
      conflict = true; guardDraft(true); error = 'This draft changed in another window. Copy any unsaved text, then reload the saved draft before submitting.'; updateNotice(); updateControls(); return false;
    }
    draft.revision = uuid();
    try { store[revisionKey] = draft; localStorage.setItem(storageKey, JSON.stringify(store)); storedRevision = draft.revision; storageError = ''; guardDraft(false); return true; }
    catch { guardDraft(true); storageError = 'This draft could not be saved on this Mac. Copy your text, then reload the saved draft to discard unsaved text before closing.'; updateNotice(); return false; }
  }
  function restoreDraft() {
    const store = readStore(), saved = store[revisionKey];
    draft = saved && typeof saved.body === 'string' && Array.isArray(saved.comments) ? saved : emptyDraft();
    storedRevision = saved?.revision || ''; conflict = false; storageError = ''; error = ''; guardDraft(false);
    if (!draft.body && pendingReview?.body) draft.body = pendingReview.body;
    summary.value = draft.body; action.value = draft.event || 'COMMENT'; renderEarlier(store);
    hideComposer(); updateControls();
  }
  function renderEarlier(store) {
    const old = Object.entries(store).filter(([key, value]) => key !== revisionKey && (value.body || value.comments?.length || value.composer?.body));
    earlier.hidden = !old.length; earlier.replaceChildren(el('summary', '', `${old.length} draft${old.length === 1 ? '' : 's'} from earlier PR revisions`));
    for (const [key, value] of old) {
      const text = input('textarea', `Saved draft for revision ${key}`, 'pr-old-draft'); text.readOnly = true; text.rows = 6;
      text.value = [`Revision ${key}`, value.body, ...(value.comments || []).map(c => `${c.path} · ${c.side} ${c.start_line ? c.start_line + '–' : ''}${c.line}\n${c.body}`), value.composer?.body || ''].filter(Boolean).join('\n\n');
      earlier.append(el('p', 'pr-muted', 'These locations belong to an earlier diff. Copy the text and select its location again in the current diff.'), text);
    }
  }
  function requestPR(operation, data = {}) {
    return Promise.resolve(request({operation, prLoadID:loadID, ...data})).then(result => { if (result === false) throw new Error('Could not send the request to DevJay.'); });
  }
  function load() {
    if (!view?.reviewPRs?.length || busy || draft?.pending || draft?.commentWrite || (draft && !persist())) return;
    loading = true; loadFailed = false; error = ''; loadID++; snapshot = null; pendingReview = null; draft = null; hideComposer(); renderDrafts();
    requestPR('pr-review-load', {prNumber:Number(choosePR.value)}).catch(() => { loading = false; loadFailed = true; error = 'Could not load PR details. Use Refresh PR details to retry.'; updateControls(); });
  }
  function sync(next) {
    view = next;
    const links = next.reviewPRs || [], key = JSON.stringify([next.path, next.branch, links.map(p=>p.number)]);
    controls.hidden = !links.length && !draft;
    if (key !== identity && !busy && !draft?.pending && !draft?.commentWrite && !draftGuard) {
      identity = key; choosePR.replaceChildren(...links.map(pr => { const option = el('option', '', `PR #${pr.number}`); option.value = String(pr.number); return option; }));
      choosePR.value = String(links[0]?.number || ''); choosePR.hidden = links.length < 2;
      if (links.length) load();
    }
    frame.classList.toggle('br-pr-comments', !!links.length); updateControls();
  }
  function updateNotice() {
    notice.textContent = [error, storageError, success].filter(Boolean).join(' '); notice.hidden = !notice.textContent;
  }
  function updateControls() {
    const count = (draft?.comments?.length || 0) + (pendingReview?.comments?.length || 0);
    finish.textContent = count ? `Review comments (${count})` : 'Review comments';
    finish.disabled = !!view?.reviewEditing; reload.disabled = busy || loading || !!draft?.pending || !!draft?.commentWrite;
    choosePR.disabled = busy || loading || !!draft?.pending || !!draft?.composer || !!draft?.commentWrite;
    publish.textContent = busy && !draft?.commentWrite ? 'Submitting…' : draft?.pending ? 'Check submission' : 'Submit review';
    publish.disabled = !snapshot || busy || loading || conflict || !!draft?.composer || !composer.hidden || !!draft?.commentWrite || !!draft?.comments?.length;
    retryLabel.hidden = retrySubmission.hidden = !draft?.pending || busy;
    retrySubmission.disabled = !retryConfirmed.checked || publish.disabled;
    action.disabled = busy || !!draft?.pending; summary.readOnly = busy || !!draft?.pending || !snapshot;
    saveComment.disabled = !snapshot || anchorPending || !draft?.composer?.location || busy || conflict || !!draft?.pending || !!draft?.commentWrite || !comment.value.trim();
    cancelComment.disabled = busy || !!draft?.commentWrite; comment.readOnly = !snapshot || busy || !!draft?.pending || !!draft?.commentWrite;
    commentRecovery.hidden = !draft?.commentWrite; checkComment.disabled = busy; retryComment.disabled = busy || !retryCommentConfirmed.checked;
    sharedReload.hidden = !conflict && !storageError;
    for (const option of action.children) option.disabled = option.value !== 'COMMENT' && snapshot?.author?.toLowerCase() === snapshot?.viewer?.toLowerCase();
    account.textContent = snapshot ? `PR #${snapshot.number} · ${snapshot.viewer} · ${pendingReview ? 'Pending review on GitHub' : 'Your first comment starts a review'}. Comments stay private until submission.` : loading ? 'Loading PR details…' : 'PR details are unavailable. Refresh to try again.';
    if (snapshot) github.href = snapshot.url;
    updateNotice();
  }
  function decorateGutter(node, info) {
    node.prSource = info.text; node.dataset.prPath = info.path; node.dataset.prSide = info.side; node.dataset.prLine = String(info.line);
    node.classList.add('br-comment-gutter'); node.tabIndex = 0; node.setAttribute('role', 'button');
    node.setAttribute('aria-label', `Add PR comment on ${info.path}, ${info.side === 'LEFT' ? 'original' : 'new'} line ${info.line}`);
    node.title = 'Add PR review comment · Shift-click another line for a range';
    node.onclick = event => {
      event?.stopPropagation?.();
      if (!view?.reviewPRs?.length || view.reviewEditing || busy || draft?.pending || draft?.commentWrite) return;
      if (draft?.composer?.body?.trim() || (!composer.hidden && comment.value.trim())) { error = 'Add or cancel the current comment first. Use Review comments to continue an unfinished comment.'; updateNotice(); return; }
      error = '';
      const next = {...info};
      if (event?.shiftKey && rangeStart?.path === info.path && rangeStart.side === info.side && rangeStart.text === info.text) {
        next.startLine = Math.min(info.line, rangeStart.line); next.line = Math.max(info.line, rangeStart.line);
      }
      rangeStart = info; openInline(next);
    };
    node.onkeydown = event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); node.onclick(event); } };
  }
  function attachComposer() {
    if (!target || composer.hidden) return;
    const gutter = [...frame.querySelectorAll('.br-comment-gutter')].find(n => n.dataset.prPath === target.path && n.dataset.prSide === target.side && Number(n.dataset.prLine) === target.line && n.prSource === target.text);
    if (!gutter) { if (!panel.contains(composer)) composer.remove(); return; }
    let row = gutter; while (row && !row.classList.contains('br-row')) row = row.parentNode;
    if (row?.parentNode) row.parentNode.insertBefore(composer, row.nextSibling);
  }
  function attachDrafts() {
    for (const node of frame.querySelectorAll('.pr-inline-draft')) node.remove();
    const inline = allComments().filter(c => c.local);
    if (!inline.length) return;
    const gutters = [...frame.querySelectorAll('.br-comment-gutter')];
    for (const c of inline) {
      let row = gutters.find(n => n.dataset.prPath === c.local.path && n.dataset.prSide === c.local.side && Number(n.dataset.prLine) === c.local.line && n.prSource === c.local.text);
      while (row && !row.classList.contains('br-row')) row = row.parentNode;
      if (row?.parentNode) { const node = draftCard(c); node.classList.add('pr-inline-draft'); row.parentNode.insertBefore(node, row.nextSibling); }
    }
  }
  function openInline(info, id = '', body = '', knownLocation = null) {
    target = info; pendingClick = {info, id, body, knownLocation};
    fillHeader(location, {path:info.path,side:info.side,line:info.line,start_line:info.startLine});
    comment.value = body; composer.hidden = false; saveComment.textContent = id ? 'Save comment' : 'Add review comment';
    composerStatus.textContent = snapshot ? 'Checking the PR line…' : 'Loading PR details…'; attachComposer();
    if (snapshot) beginAnchor(); else if (!loading && !loadFailed) load();
    updateControls(); comment.focus(); composer.scrollIntoView({block:'nearest'});
  }
  function beginAnchor() {
    if (!pendingClick || !draft || !snapshot) return;
    const {info,id,body,knownLocation} = pendingClick; pendingClick = null;
    draft.composer = {local:info, id, body, location:knownLocation}; persist();
    if (knownLocation) { anchorPending = false; describeLocation(knownLocation); updateControls(); return; }
    anchorPending = true; const idRequest = ++anchorID;
    requestPR('pr-review-anchor', {token:snapshot.token, prAnchor:{id:idRequest, path:info.path, side:info.side, line:info.line, startLine:info.startLine || 0, text:info.text}}).catch(() => { if (anchorID !== idRequest) return; anchorPending = false; composerStatus.textContent = 'Could not check this line. Cancel and try again.'; updateControls(); });
  }
  function hideComposer() { composer.hidden = true; composer.remove(); target = null; comment.value = ''; anchorPending = false; }
  function describeLocation(location) {
    const label = location?.summary ? 'Add to review summary' : draft?.composer?.id ? 'Save comment' : 'Add review comment';
    saveComment.textContent = label;
    saveComment.setAttribute('aria-label', location?.summary ? label : 'Save inline comment to draft review');
    composerStatus.textContent = location?.summary
      ? (location.permalink ? 'Your review summary will include a link to these lines.' : 'Your review summary will reference the file and line numbers as shown locally.')
      : 'Add saves this inline comment to your private pending review on GitHub.';
  }
  function allComments() {
    return [...(pendingReview?.comments || []).map(c=>({...c,id:`github-${c.id}`,remoteID:c.id,local:draft?.locations?.[c.id]})), ...(draft?.comments || [])];
  }
  function queueComment(action, c, local, localID = '') {
    if (!snapshot || !draft || busy || draft.pending || draft.commentWrite || conflict) return;
    if (draft.composer && (action === 'delete' || (localID && draft.composer.id !== localID))) {
      error = 'Add or cancel your unfinished comment first.'; updateNotice(); return;
    }
    draft.commentWrite = {token:snapshot.token,requestID:uuid(),action,reviewID:pendingReview?.id||0,commentID:c.remoteID||0,originalBody:c.originalBody||c.body||'',comment:{path:c.path,line:c.line,side:c.side,body:c.body,...(c.start_line?{start_line:c.start_line,start_side:c.start_side}:{})},local,localID};
    if(!persist()){draft.commentWrite=null;updateControls();return;}
    sendComment(false);
  }
  function sendComment(checkOnly = true, retry = false) {
    if (!draft?.commentWrite || busy || conflict) return;
    const change=draft.commentWrite;
    const reconcile=checkOnly || (change.checkOnly && !retry); change.checkOnly=true;
    if(!persist())return;
    busy=true;error='';panel.hidden=false;updateControls();retryCommentConfirmed.checked=false;
    const {local,localID,checkOnly:ignore,...prComment}=change;prComment.token=snapshot.token;
    requestPR(retry?'pr-review-comment-retry':reconcile?'pr-review-comment-check':'pr-review-comment',{prComment}).catch(()=>{busy=false;error='Comment save could not be confirmed. Use Check comment status.';updateControls();});
  }
  function saveInline() {
    if (!draft?.composer?.location || anchorPending || busy || draft.pending || draft.commentWrite || conflict || !comment.value.trim()) return;
    const c=draft.composer, saved=allComments().find(n=>n.id===c.id);
    if (c.location.summary) {
      const ref = c.location;
      const lines = ref.start_line ? `${ref.start_line}–${ref.line}` : String(ref.line);
      const label = `${ref.path}:${lines}`.replace(/[\r\n\t]/g, ' ').replace(/[\\\[\]`*_~<>|&]/g, '\\$&');
      const source = ref.permalink ? `[${label}](<${ref.permalink}>)` : `${label} (${ref.side === 'LEFT' ? 'original' : 'working copy'}, local line numbers)`;
      const body = [draft.body, `${source}\n\n${comment.value}`].filter(Boolean).join('\n\n');
      if (new TextEncoder().encode(body).length > 60000) {
        error = 'The combined review summary exceeds 60,000 bytes. Shorten the summary or this comment and try again.'; updateNotice(); return;
      }
      const previous = draft.body;
      draft.body = body; draft.composer = null;
      if (!persist()) { draft.body = previous; draft.composer = c; updateControls(); return; }
      hideComposer(); error = ''; success = 'Comment added to the summary. Submit review to share it on GitHub.';
      panel.hidden = false; renderDrafts(); summary.focus(); return;
    }
    queueComment(saved?.remoteID?'update':'add',{...c.location,body:comment.value,remoteID:saved?.remoteID,originalBody:saved?.body},c.local,saved&&!saved.remoteID?saved.id:'');
  }
  function locationText(c) { return `${c.path} · ${c.side === 'LEFT' ? 'original' : 'new'} ${c.start_line ? c.start_line+'–' : ''}${c.line}`; }
  function fillHeader(header, c) {
    const path=el('strong','pr-comment-path',c.path), lines=el('span','pr-comment-lines',`${c.side==='LEFT'?'Original':'New'} · ${c.start_line&&c.start_line!==c.line?c.start_line+'–':''}${c.line || 'File'}`);
    path.title=c.path;header.replaceChildren(path,lines);
  }
  function draftCard(c) {
    const node=el('article','pr-draft pr-review-card'), header=el('header','pr-comment-header');fillHeader(header,c);
    const content=el('div','pr-comment-content');content.append(el('p','pr-comment-body',c.body));
    const actions=el('footer','pr-comment-actions');actions.append(el('span','pr-comment-status',c.outdated?'Outdated':c.remoteID?'Pending':'Not yet saved to GitHub'));
    node.append(header,content,actions);
    const edit=button('Edit',`Edit draft comment on ${locationText(c)}`,()=>{
      if (draft.composer?.body) {error='Add or cancel your unfinished comment first.';updateNotice();return;}
      const local=c.local || {path:c.path,side:c.side,line:c.line,startLine:c.start_line};
      openInline(local,c.id,c.body,{path:c.path,side:c.side,line:c.line,...(c.start_line?{start_line:c.start_line,start_side:c.start_side}:{})});
      // Editing a saved comment remains available even when its file is offscreen.
      if (!composer.parentNode) {panel.hidden=false;draftList.append(composer);comment.focus();}
    });
    const remove=button('Delete',`Delete draft comment on ${locationText(c)}`,()=>{if(draft.composer){error='Add or cancel your unfinished comment first.';updateNotice();return;}if(c.remoteID)queueComment('delete',c,c.local);else{draft.comments=draft.comments.filter(n=>n.id!==c.id);persist();renderDrafts();}});
    if(!c.remoteID){const save=button('Add to pending review',`Save local comment on ${locationText(c)} to GitHub`,()=>queueComment('add',c,c.local,c.id));save.disabled=busy||!!draft?.commentWrite;actions.append(save);}
    edit.disabled=remove.disabled=busy||!!draft?.pending||!!draft?.commentWrite;actions.append(edit,remove);return node;
  }
  function renderDrafts() {
    if (!draft) {draftList.replaceChildren();updateControls();return;}
    const panelComposer = draftList.contains(composer) && !composer.hidden;
    const focused = panelComposer && composer.contains(document.activeElement);
    if (panelComposer) composer.remove();
    draftList.replaceChildren(...allComments().map(draftCard));
    if (draft.composer) {
      const resume=button('Continue unfinished comment','Continue unfinished PR comment',()=>{
        const c=draft.composer, local=c.local || {path:c.location.path,side:c.location.side,line:c.location.line};
        openInline(local,c.id||'',c.body||'',c.location);
        if (!composer.parentNode) {panel.hidden=false;draftList.append(composer);comment.focus();}
      });draftList.append(resume);
    } else if (!allComments().length) draftList.append(el('p','pr-muted','Add an inline comment by clicking a line number in the diff, or write a summary below.'));
    if (panelComposer) {draftList.append(composer);if(focused)comment.focus({preventScroll:true});}
    summary.value=draft.body;action.value=draft.event;attachDrafts();updateControls();
  }
  function submit(retry = false) {
    // DOM onclick passes an event; only the explicit retry button passes true.
    retry = retry === true;
    if (!snapshot || !draft || busy || conflict || draft.commentWrite || draft.comments.length || !composer.hidden) return;
    if (!draft.pending) {
      draft.body = summary.value; draft.event = action.value;
      if ((draft.event === 'REQUEST_CHANGES' || (draft.event === 'COMMENT' && !pendingReview?.comments?.length && !draft.comments.length)) && !draft.body.trim()) { error = 'Write a review summary before submitting.'; updateNotice(); summary.focus(); return; }
      draft.pending = true;
      draft.reviewID = pendingReview?.id || 0; draft.pendingVersion = pendingReview?.version || '';
      if (!persist()) { draft.pending = false; updateControls(); return; }
    }
    if (retry && (!draft.pending || !retryConfirmed.checked)) return;
    const checkOnly = !!draft.checkOnly && !retry;
    // Persist the uncertain state BEFORE sending. A reload/restart can only
    // reconcile this request, never silently replay the POST.
    draft.checkOnly = true;
    if (!persist()) { updateControls(); return; }
    busy = true; error = ''; updateControls(); renderDrafts();
    const prSubmission = {reviewID:draft.reviewID||0,pendingVersion:draft.pendingVersion||'',token:snapshot.token, requestID:draft.requestID, event:draft.event, body:draft.body, comments:draft.comments.map(({id, local, ...c}) => c)};
    retryConfirmed.checked = false;
    requestPR(retry ? 'pr-review-retry' : checkOnly ? 'pr-review-check' : 'pr-review-submit', {prSubmission}).catch(() => { busy = false; error = 'Submission could not be confirmed. Use Check submission before trying again.'; updateControls(); });
  }
  function receive(state) {
    if (!state || state.loadID !== loadID) return;
    loading=!!state.loading;busy=!!state.submitting;
    const previousPending = pendingReview;
    if ('pending' in state) pendingReview=state.pending; else if(state.snapshot)pendingReview=state.snapshot.pending||null;
    if (state.snapshot && state.snapshot.token !== snapshot?.token) {
      snapshot=state.snapshot;storageKey=`devjay-pr-review:${snapshot.repo}#${snapshot.number}`;revisionKey=`${snapshot.head}:${snapshot.base}`;
      const queued=pendingClick;restoreDraft();
      if (queued && !draft.composer?.body) openInline(queued.info,queued.id,queued.body,queued.knownLocation);
      renderDrafts(); if(pendingReview?.comments?.length || draft?.commentWrite)panel.hidden=false;
    }
    if (anchorPending && state.anchor?.id === anchorID && draft?.composer) {
      anchorPending=false;draft.composer.location=state.anchor.location || null;persist();
      describeLocation(state.anchor.location);
      if (state.anchor.error) composerStatus.textContent = state.anchor.error;
    }
    if (state.error) {error=state.error;loadFailed=!snapshot;}
    if(draft?.commentWrite && state.comment?.requestID===draft.commentWrite.requestID && !busy){
      const change=draft.commentWrite;
      if(state.comment.done){
        const saved=pendingReview?.comments?.find(c=>c.requestID===change.requestID);
        draft.locations||={};if(saved&&change.local)draft.locations[saved.id]=change.local;
        if(change.action==='delete')delete draft.locations[change.commentID];
        if(change.localID)draft.comments=draft.comments.filter(c=>c.id!==change.localID);
        draft.commentWrite=null;if(change.action!=='delete'){draft.composer=null;hideComposer();}
        error='';success=change.action==='delete'?'Comment removed from your pending review.':'Inline comment saved to your pending review.';persist();renderDrafts();panel.hidden=false;
      }else if(state.comment.error){error=state.comment.error;if(!state.comment.unknown){draft.commentWrite=null;persist();}renderDrafts();}
    } else if(previousPending!==pendingReview && draft){renderDrafts();}
    if (draft && state.requestID===draft.requestID && !busy) {
      if (state.receiptID) {success=`Review submitted to PR #${snapshot.number}.`;pendingReview=null;draft=emptyDraft();persist();hideComposer();renderDrafts();}
      else {draft.pending=!!state.unknown;draft.checkOnly=!!state.unknown;persist();}
    }
    updateControls();
  }
  comment.oninput=()=>{if(draft?.composer){draft.composer.body=comment.value;persist();}updateControls();};
  summary.oninput=()=>{if(draft){draft.body=summary.value;persist();}};
  action.onchange=()=>{if(draft){draft.event=action.value;persist();}};
  choosePR.onchange=load;retryConfirmed.onchange=updateControls;retryCommentConfirmed.onchange=updateControls;
  for (const node of [panel,composer]) node.addEventListener('keydown',event=>{event.stopPropagation();if(event.key==='Escape'&&node===panel){event.preventDefault();panel.hidden=true;}});
  return {sync,receive,decorateGutter,beforeRender(card){restoreFocus ||= card.contains(document.activeElement)&&composer.contains(document.activeElement);if(card.contains(composer))composer.remove();},afterRender(){attachDrafts();attachComposer();if(restoreFocus&&composer.parentNode){comment.focus({preventScroll:true});restoreFocus=false;}},destroy(){if(draft)persist();composer.remove();panel.remove();controls.remove();notice.remove();}};
}
