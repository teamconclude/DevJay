const pathEllipsis = '...';
const pathSegmenter = new Intl.Segmenter(undefined, {granularity:'grapheme'});
const pathCharacters = text => Array.from(pathSegmenter.segment(text), part => part.segment);

// Keep the Site name whole before spending any space on its parent directory.
// Measuring the final strings accounts for proportional fonts and bold names.
export function fitPathLabel(prefix, name, width, measurePrefix, measureName) {
  if (!(width > 0)) return {prefix:'', name:''};
  const nameWidth = measureName(name);
  if (measurePrefix(prefix) + nameWidth <= width) return {prefix, name};
  const longest = (length, candidate, measure, available) => {
    let low=0, high=length;
    while (low<high) {
      const middle=Math.ceil((low+high)/2);
      if (measure(candidate(middle))<=available) low=middle; else high=middle-1;
    }
    return candidate(low);
  };
  if (nameWidth <= width) {
    const parts=pathCharacters(prefix),available=width-nameWidth;
    // If even the slash and omission marker don't fit, show just the name.
    if (!parts.length || measurePrefix(pathEllipsis+parts.at(-1))>available) return {prefix:'',name};
    const suffix=count=>pathEllipsis+parts.slice(parts.length-count).join('');
    return {prefix:longest(parts.length-1,suffix,measurePrefix,available),name};
  }
  const parts=pathCharacters(name);
  if (measureName(pathEllipsis)>width) return {prefix:'',name:measureName('…')<=width?'…':''};
  const middle=count=>parts.slice(0,Math.ceil(count/2)).join('')+pathEllipsis+parts.slice(parts.length-Math.floor(count/2)).join('');
  return {prefix:'',name:longest(parts.length-1,middle,measureName,width)};
}

// One observer per document, no polling. Original text stays separate so a
// widened card can recover its complete path after any number of refits.
export function installPathLabels(root) {
  const canvas=document.createElement('canvas'),context=canvas.getContext('2d');
  const labels=new Map();let frame=0;
  const schedule=()=>{if(!frame)frame=requestAnimationFrame(fit);};
  const observer=new ResizeObserver(schedule);
  function fit() {
    frame=0;
    for(const node of labels.keys())if(!root.contains(node)){observer.unobserve(node);labels.delete(node);}
    for(const node of root.querySelectorAll('.path[data-path-name]'))if(!labels.has(node)){labels.set(node,'');observer.observe(node);}
    for(const [node,previous] of labels){
      const width=Math.max(0,node.getBoundingClientRect().width-0.5);
      if(!width)continue;
      const prefix=node.querySelector('.prefix'),name=node.querySelector('strong');
      if(!prefix||!name)continue;
      const styles=[getComputedStyle(prefix),getComputedStyle(name)];
      const fonts=styles.map(s=>`${s.fontStyle} ${s.fontWeight} ${s.fontSize} ${s.fontFamily}`);
      const key=JSON.stringify([width,node.dataset.pathPrefix,node.dataset.pathName,fonts,styles.map(s=>s.letterSpacing)]);
      if(key===previous)continue;
      const measure=index=>text=>{
        context.font=fonts[index];
        const spacing=parseFloat(styles[index].letterSpacing)||0;
        return context.measureText(text).width+(spacing?spacing*Math.max(0,pathCharacters(text).length-1):0);
      };
      const fitted=fitPathLabel(node.dataset.pathPrefix||'',node.dataset.pathName||'',width,measure(0),measure(1));
      if(prefix.textContent!==fitted.prefix)prefix.textContent=fitted.prefix;
      if(name.textContent!==fitted.name)name.textContent=fitted.name;
      labels.set(node,key);
    }
  }
  new MutationObserver(records=>{
    // Ignore our own text writes; new/removed elements need discovery/cleanup.
    if(records.some(r=>r.type==='attributes'||[...r.addedNodes,...r.removedNodes].some(n=>n.nodeType===1)))schedule();
  }).observe(root,{childList:true,subtree:true,attributes:true,attributeFilter:['data-path-prefix','data-path-name']});
  document.fonts?.ready.then(schedule);
  schedule();
  return schedule;
}
