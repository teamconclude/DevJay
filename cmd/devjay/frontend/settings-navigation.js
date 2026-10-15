const pages = [
  ['sites','Sites','M3 7V4h6l2 3h10v13H3z'],
  ['profiles','Profiles','M3 4h18v12H3z M8 21h8 M12 16v5'],
  ['windows','Windows & Cards','M3 4h15v12H3z M7 16v4h14V8h-3 M3 8h15'],
  ['editors','Editors & Apps','m8 6-6 6 6 6 m8-12 6 6-6 6 m-3-14-2 16'],
  ['shortcuts','Keyboard Shortcuts','M2 6h20v12H2z M5 9h1 m3 0h1 m3 0h1 m3 0h1 M5 12h1 m3 0h1 m3 0h1 m3 0h1 M7 15h10'],
];

export function mountSettingsNavigation(root) {
  const make=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';if(text)n.textContent=text;return n;};
  const sidebar=make('nav','settings-sidebar');sidebar.setAttribute('aria-label','Settings pages');
  sidebar.append(make('div','settings-brand','DevJay'),make('div','settings-caption','Settings'));
  const content=make('div','settings-main'),scope=make('div','settings-scope'),body=make('div','settings-pages'),footer=make('footer','settings-footer');
  const sections={},buttons={};
  const show=id=>{
    if(!sections[id])return;
    for(const [key,section] of Object.entries(sections)){
      section.hidden=key!==id;buttons[key].setAttribute('aria-current',key===id?'page':'false');
    }
    scope.hidden=id==='profiles'||id==='shortcuts';body.scrollTop=0;
    try{sessionStorage.setItem('devjay-settings-page',id);}catch{}
  };
  for(const [id,title,path] of pages){
    const button=make('button','settings-nav-item');button.type='button';button.id=`settings-nav-${id}`;
    const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('viewBox','0 0 24 24');svg.setAttribute('aria-hidden','true');
    const shape=document.createElementNS(svg.namespaceURI,'path');shape.setAttribute('d',path);svg.append(shape);
    button.append(svg,make('span','',title));button.onclick=()=>show(id);sidebar.append(button);buttons[id]=button;
    const section=make('section','settings-page');section.id=`settings-page-${id}`;section.setAttribute('aria-labelledby',button.id);sections[id]=section;body.append(section);
  }
  content.append(scope,body,footer);root.append(sidebar,content);
  let initial='sites';try{initial=sessionStorage.getItem('devjay-settings-page')||initial;}catch{}
  show(sections[initial]?initial:'sites');
  return {sections,scope,footer,show};
}
