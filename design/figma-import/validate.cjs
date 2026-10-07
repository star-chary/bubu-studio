// Structural simulation only. Does not emulate Figma rendering or permissions.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');
const nodes = new Map();
let next = 1;
class Node {
  constructor(type) {
    this.id = 'test:' + next++; this.type = type; this.children = []; this.name = '';
    this.reactions = []; this.width = 100; this.height = 100; this.x = 0; this.y = 0;
    nodes.set(this.id, this);
  }
  appendChild(n) {
    if (n.parent) n.parent.children = n.parent.children.filter(x => x !== n);
    this.children.push(n); n.parent = this;
  }
  resize(w,h) { assert(w > 0 && h > 0); this.width=w; this.height=h; }
  addComponentProperty(name,type,value) {
    assert.equal(type,'TEXT'); const key = name + '#' + this.id;
    this.propertyKey=key;this.propertyDefault=value;return key;
  }
  createInstance() {
    assert.equal(this.type,'COMPONENT');
    const instance=new Node('INSTANCE');instance.mainComponent=this;
    instance.setProperties = props => {
      assert(Object.hasOwn(props,this.propertyKey)); instance.label=props[this.propertyKey];
    };
    return instance;
  }
  async setReactionsAsync(list) {
    for(const r of list){
      assert(['ON_CLICK','AFTER_TIMEOUT','ON_KEY_DOWN'].includes(r.trigger.type));
      if(r.trigger.type==='AFTER_TIMEOUT')assert(r.trigger.timeout>0);
      for(const action of r.actions){assert(nodes.has(action.destinationId));assert.equal(action.navigation,'NAVIGATE');}
    }
    this.reactions=list;
  }
}
const root=new Node('DOCUMENT');
let finish;
const finished=new Promise(resolve=>finish=resolve);
const figma={
  root,currentPage:null,
  async listAvailableFontsAsync(){return [{fontName:{family:'Noto Sans SC',style:'Regular'}}];},
  async loadFontAsync(){},
  createPage(){const p=new Node('PAGE');root.appendChild(p);return p;},
  async setCurrentPageAsync(p){this.currentPage=p;},
  createFrame(){return new Node('FRAME');},
  createRectangle(){return new Node('RECTANGLE');},
  createText(){return new Node('TEXT');},
  createComponent(){return new Node('COMPONENT');},
  createNodeFromSvg(svg){assert(svg.includes('viewBox='));return new Node('FRAME');},
  createImage(bytes){assert(bytes.length>0);return {hash:'test-image'};},
  base64Decode(s){return new Uint8Array(Buffer.from(s,'base64'));},
  variables:{
    createVariableCollection(name){return {name,defaultModeId:'mode:1'};},
    createVariable(name,col,type){assert.equal(type,'COLOR');return {name,setValueForMode(){},setVariableCodeSyntax(){}};},
    setBoundVariableForPaint(p,field,variable){assert.equal(field,'color');return {...p,boundVariables:{color:variable.name}};}
  },
  viewport:{scrollAndZoomIntoView(n){assert(n.length>0);}},
  closePlugin(message){finish(message);}
};
async function run(){
  const manifest=JSON.parse(fs.readFileSync(path.join(__dirname,'manifest.json'),'utf8'));
  assert.deepEqual(manifest.networkAccess.allowedDomains,['none']);
  const script=fs.readFileSync(path.join(__dirname,'code.js'),'utf8');
  vm.runInNewContext(script,{figma,console},{timeout:10000});
  const message=await finished;
  assert(!message.startsWith('导入未完成'),message);
  const page=root.children.find(p=>p.name==='V1 · 完整交互原型');assert(page);
  const screens=page.children.filter(n=>/^\d+ · |^开始这里/.test(n.name));
  assert.equal(screens.length,26);
  const screenIds=new Set(screens.map(n=>n.id));
  const connections=[];
  for(const n of nodes.values())for(const r of n.reactions)for(const a of r.actions){
    assert(screenIds.has(a.destinationId));connections.push({source:n,target:a.destinationId,trigger:r.trigger});
  }
  function owner(n){while(n&&!screenIds.has(n.id))n=n.parent;return n;}
  const graph=new Map(screens.map(n=>[n.id,new Set()]));
  for(const edge of connections){const parent=owner(edge.source);assert(parent);graph.get(parent.id).add(edge.target);}
  const start=page.flowStartingPoints[0].nodeId,visited=new Set(),todo=[start];
  while(todo.length){const id=todo.pop();if(visited.has(id))continue;visited.add(id);todo.push(...graph.get(id));}
  assert.equal(visited.size,screens.length,'Every screen should be reachable from the guide');
  const ready=screens.find(n=>n.name==='07 · 提示词就绪');
  const expanded=screens.find(n=>n.name==='08 · 放大编辑');
  const texts=n=>{const out=[];const walk=x=>{if(x.type==='TEXT')out.push(x.characters);if(x.label)out.push(x.label);x.children.forEach(walk);};walk(n);return out;};
  const citation='@陈志远_角色三视图.png';
  assert(texts(ready).includes(citation));assert(texts(expanded).includes(citation));
  assert(texts(ready).includes('保持人物造型，生成雨夜街头近景，背景有柔和的霓虹灯光。'));
  assert(texts(expanded).includes('保持人物造型，生成雨夜街头近景，背景有柔和的霓虹灯光。'));
  for(const n of screens)assert.equal(n.width,1440);
  for(const n of screens)assert.equal(n.height,900);
  const summary={screens:screens.length,connections:connections.length,allScreensReachable:true,expandPreservesExampleContent:true,networkAccess:'none',figmaRuntimeVerified:false};
  fs.writeFileSync(path.join(__dirname,'validation.json'),JSON.stringify(summary,null,2)+'\n');
  console.log(JSON.stringify(summary,null,2));
}
run().catch(error=>{console.error(error);process.exitCode=1;});
