/* One-time, editable Figma prototype. Uploads, typing, generation and downloads
 * are scripted demonstrations. This is not application or backend code. */
async function main() {
  const PAGE = 'V1 · 完整交互原型';
  const existing = figma.root.children.find(p => p.name === PAGE);
  if (existing) {
    await figma.setCurrentPageAsync(existing);
    figma.closePlugin('已存在完整原型页面，已切换；未覆盖你的修改。');
    return;
  }
  const available = await figma.listAvailableFontsAsync();
  const regular = available.find(f => f.fontName.family === 'Noto Sans SC' && f.fontName.style === 'Regular')
    || available.find(f => /Microsoft YaHei|PingFang SC/.test(f.fontName.family) && /Regular/.test(f.fontName.style));
  if (!regular) throw new Error('请先安装 Noto Sans SC 中文字体，再运行导入。尚未创建页面。');
  const font = regular.fontName;
  await figma.loadFontAsync(font);
  const page = figma.createPage();
  page.name = PAGE + ' · 构建中';
  await figma.setCurrentPageAsync(page);

  const RGB = hex => ({r: parseInt(hex.slice(1,3),16)/255, g: parseInt(hex.slice(3,5),16)/255, b: parseInt(hex.slice(5,7),16)/255});
  const palette = {canvas:'#111214', panel:'#26282B', raised:'#33363B', border:'#3B3E43', text:'#EEEEF0', muted:'#ADB1BA', accent:'#4F8EFF', accentSurface:'#21365C', success:'#79D8A8', danger:'#F79B9B'};
  const tokens = figma.variables.createVariableCollection('帧间 V1 / Dark');
  const vars = {};
  for (const [name,hex] of Object.entries(palette)) {
    const variable = figma.variables.createVariable(name,tokens,'COLOR');
    variable.scopes = ['FRAME_FILL','SHAPE_FILL','TEXT_FILL','STROKE_COLOR'];
    variable.setValueForMode(tokens.defaultModeId,RGB(hex));
    variable.setVariableCodeSyntax('WEB','var(--'+name+')');
    vars[name] = variable;
  }
  function fill(token) {
    const p = {type:'SOLID',color:RGB(palette[token] || token)};
    return [vars[token] ? figma.variables.setBoundVariableForPaint(p,'color',vars[token]) : p];
  }
  function frame(parent,name,x,y,w,h,color='panel',radius=12) {
    const n = figma.createFrame();parent.appendChild(n);n.name=name;n.resize(w,h);n.x=x;n.y=y;
    n.fills=color?fill(color):[];n.cornerRadius=radius;n.clipsContent=false;return n;
  }
  function layout(parent,name,vertical=false,gap=8) {
    const n=figma.createFrame();parent.appendChild(n);n.name=name;n.layoutMode=vertical?'VERTICAL':'HORIZONTAL';
    n.primaryAxisSizingMode='AUTO';n.counterAxisSizingMode='AUTO';n.itemSpacing=gap;n.fills=[];
    n.counterAxisAlignItems='CENTER';n.clipsContent=false;return n;
  }
  function text(parent,name,value,size=14,color='text',width) {
    const n=figma.createText();parent.appendChild(n);n.name=name;n.fontName=font;n.fontSize=size;
    n.lineHeight={unit:'PERCENT',value:150};n.fills=fill(color);n.characters=value;
    if(width){n.textAutoResize='HEIGHT';n.resize(width,n.height);}return n;
  }
  function place(n,x,y){n.x=x;n.y=y;return n;}
  function padding(n,v){n.paddingLeft=v;n.paddingRight=v;n.paddingTop=v;n.paddingBottom=v;}

  // Reusable editable button components; instances retain their labels/properties.
  const masters = frame(page,'组件 / Buttons',200,-1050,960,180,'panel');
  const buttons={};
  for(const [index,kind] of ['Default','Primary','Quiet'].entries()) {
    const c=figma.createComponent();masters.appendChild(c);c.name='Button / '+kind;c.x=32+index*270;c.y=60;
    c.layoutMode='HORIZONTAL';c.primaryAxisSizingMode='AUTO';c.counterAxisSizingMode='AUTO';padding(c,10);
    c.paddingLeft=14;c.paddingRight=14;c.itemSpacing=8;c.counterAxisAlignItems='CENTER';c.cornerRadius=8;
    c.fills=kind==='Quiet'?[]:fill(kind==='Primary'?'text':'raised');
    c.description='原型按钮；Label 属性可编辑。点击交互设置在各实例上。';
    const label=text(c,'Label','按钮',13,kind==='Primary'?'canvas':'text');
    const property=c.addComponentProperty('Label','TEXT','按钮');label.componentPropertyReferences={characters:property};
    buttons[kind]={component:c,property};
  }
  text(masters,'LibraryTitle','基础组件 · 修改主组件可同步按钮样式',14).x=32;
  function button(parent,name,label,kind='Default') {
    const m=buttons[kind], n=m.component.createInstance();parent.appendChild(n);n.name=name;
    n.setProperties({[m.property]:label});return n;
  }
  const shadow=[{type:'DROP_SHADOW',color:{r:0,g:0,b:0,a:.28},offset:{x:0,y:12},radius:32,spread:0,visible:true,blendMode:'NORMAL'}];
  const image=figma.createImage(figma.base64Decode(REFERENCE_PNG));
  const regions={male:[173,241,315,209],female:[628,118,315,210],result:[1040,335,209,373]};
  function crop(parent,name,x,y,w,h,key) {
    const [cx,cy,cw,ch]=regions[key];const n=figma.createRectangle();parent.appendChild(n);n.name=name;
    n.resize(w,h);n.x=x;n.y=y;n.cornerRadius=8;
    n.fills=[{type:'IMAGE',imageHash:image.hash,scaleMode:'CROP',imageTransform:[[cw/2408,0,cx/2408],[0,ch/885,cy/885]]}];return n;
  }
  function vector(parent,name,svg,x=0,y=0) {
    const n=figma.createNodeFromSvg(svg);parent.appendChild(n);n.name=name;n.x=x;n.y=y;return n;
  }
  let gridPath='';for(let y=12;y<900;y+=24)for(let x=12;x<1440;x+=24)gridPath+=`M${x} ${y}h1v1h-1z `;
  const gridSvg=`<svg width="1440" height="900" viewBox="0 0 1440 900" xmlns="http://www.w3.org/2000/svg"><path d="${gridPath}" fill="#303238"/></svg>`;
  function line(parent,from,to){
    const d=`M${from[0]} ${from[1]} C${from[0]+160} ${from[1]} ${to[0]-160} ${to[1]} ${to[0]} ${to[1]}`;
    return vector(parent,'引用连线',`<svg width="1440" height="900" viewBox="0 0 1440 900" xmlns="http://www.w3.org/2000/svg"><path d="${d}" fill="none" stroke="#739ADB" stroke-width="1.5"/></svg>`);
  }
  const screens={},controls={},links=[];
  function link(node,target,trigger={type:'ON_CLICK'}){links.push({node,target,trigger});}
  function shell(key,title,index){
    const s=frame(page,title,200+(index%4)*1560,200+Math.floor(index/4)*1040,1440,900,'canvas',0);
    s.clipsContent=true;screens[key]=s;controls[key]={};vector(s,'Canvas / 点阵',gridSvg);
    const h=place(layout(s,'项目导航',false,12),24,20);
    text(h,'品牌','帧间',18);text(h,'分隔','/',16,'muted');text(h,'项目','我的漫剧',14);
    const board=button(h,'画布','画布 1 ▾');link(board,'assets');text(h,'保存状态','已保存',12,'muted');
    const badge=place(text(s,'原型标记','交互原型 · 示例数据',12,'muted'),1245,27);badge.locked=true;
    const dock=place(layout(s,'节点工具栏',false,8),455,826);padding(dock,8);dock.fills=fill('panel');dock.cornerRadius=14;
    for(const [name,label,to] of [['上传','↑ 上传资源','upload'],['图片','＋ 图片','edit'],['视频','＋ 视频','video'],['平移','平移说明','canvasHelp']]) {
      const b=button(dock,name,label);link(b,to);controls[key][name]=b;
    }
    const zoom=place(button(s,'画布缩放','－    60%    ＋','Quiet'),16,829);link(zoom,'canvasHelp');
    const help=place(button(s,'使用说明','使用说明','Quiet'),1290,829);link(help,'guide');
    return s;
  }
  function assets(s,select=false) {
    place(text(s,'角色名称','陈志远_角色三视图.png',13,'muted'),115,193);
    const male=crop(s,'素材 / 陈志远',115,220,280,186,'male');
    place(text(s,'角色名称','虞诗音_角色三视图.png',13,'muted'),440,103);
    const female=crop(s,'素材 / 虞诗音',440,130,240,160,'female');
    if(select){male.strokes=fill('accent');male.strokeWeight=2;female.opacity=.45;}
    return {male,female};
  }
  function target(s,state='empty',isVideo=false){
    place(text(s,'生成节点名称',isVideo?'视频节点 1':'图片节点 1',13),870,190);
    const n=frame(s,'当前生成节点',870,218,260,184,'panel',10);n.strokes=fill('accent');n.strokeWeight=1.5;
    const col=place(layout(n,'节点内容',true,8),40,44);
    text(col,'状态图标',state==='failed'?'!':state==='working'?'◌':isVideo?'▷':'◇',30,state==='failed'?'danger':'muted');
    text(col,'状态文案',state==='failed'?'生成失败，输入已保留':state==='working'?'正在生成，请稍候':'添加参考，开始生成',13,'muted');
    return n;
  }
  function composer(s,key,{ref=false,ready=false,expanded=false,video=false,working=false}={}){
    const p=place(layout(s,'提示词编辑区',true,12),expanded?350:715,expanded?116:432);
    p.resize(expanded?740:600,expanded?650:286);p.primaryAxisSizingMode='FIXED';p.counterAxisSizingMode='FIXED';
    p.counterAxisAlignItems='MIN';padding(p,16);p.fills=fill('panel');p.strokes=fill('border');p.cornerRadius=16;p.effects=shadow;
    const top=layout(p,'编辑区工具栏');top.layoutSizingHorizontal='FILL';top.primaryAxisAlignItems='SPACE_BETWEEN';
    const refButton=button(top,'选择参考','＋ 参考');if(!working)link(refButton,video?'videoRefs':'select');
    const expand=button(top,'放大恢复',expanded?'恢复 ↙':'放大 ↗');if(!working)link(expand,expanded?'ready':'expanded');
    const refs=layout(p,'参考缩略图');refs.layoutSizingHorizontal='FILL';refs.resize(560,52);refs.layoutSizingVertical='FIXED';
    if(ref){
      const thumb=frame(refs,'参考 / '+(video?'图片节点 1':'陈志远_角色三视图.png'),0,0,52,52,null,8);
      crop(thumb,'素材预览',0,0,52,52,video?'result':'male');
      const badge=frame(thumb,'顺序编号',2,2,17,19,'panel',4);place(text(badge,'编号','1',11),5,0);
      const label=text(refs,'参考文件名',video?'图片节点 1':'陈志远_角色三视图.png',12,'muted');
      const remove=button(refs,'移除参考','×','Quiet');if(!working)link(remove,video?'video':'edit');
    }else{text(refs,'参考说明','选择画布上的素材作为参考',12,'muted');}
    const area=layout(p,'提示词正文',true,8);area.layoutSizingHorizontal='FILL';area.layoutSizingVertical='FILL';area.counterAxisAlignItems='MIN';
    if(ready){
      const line=layout(area,'第一行',false,6);text(line,'普通文本','参考',15);
      const chip=button(line,'引用标签',video?'@图片节点 1':'@陈志远_角色三视图.png','Quiet');chip.fills=fill('accentSurface');
      if(!working)link(chip,video?'videoRefs':'mention');
      text(area,'描述',video?'保持人物造型与街景，人物缓慢向前走，镜头平稳跟随。':'保持人物造型，生成雨夜街头近景，背景有柔和的霓虹灯光。',15,'text',expanded?695:550);
    }else{
      const prompt=text(area,'占位提示','描述你想生成的画面内容，输入 @ 引用素材',15,'muted',550);
      link(prompt,ref?'mention':'select');
    }
    const bottom=layout(p,'生成参数');bottom.layoutSizingHorizontal='FILL';bottom.primaryAxisAlignItems='SPACE_BETWEEN';
    const params=layout(bottom,'参数选择',false,6);
    const model=button(params,'模型',video?'视频模型 ▾':'图像模型 ▾');link(model,'parameters');
    const ratio=button(params,'比例','9:16 ▾');link(ratio,'parameters');
    const qty=button(params,'数量时长',video?'5 秒 ▾':'1 张 ▾');link(qty,'parameters');
    const send=button(bottom,'生成',working?'生成中…':'↑ 生成','Primary');send.opacity=ready&&!working?1:.4;
    if(ready&&!working)link(send,video?'videoWorking':'working');
    controls[key].composer=p;controls[key].send=send;
    return p;
  }
  function dim(s,opacity=.6){const r=frame(s,'背景遮罩',0,0,1440,900,'#000000',0);r.opacity=opacity;return r;}
  function note(s,value){place(text(s,'操作提示',value,12,'muted'),715,737);}
  function resultNode(s,video=false){
    place(text(s,'结果名称',video?'视频节点 1 · 已完成':'图片节点 1 · 已完成',13),835,128);
    const result=crop(s,'生成结果 / 示例',835,157,218,388,'result');
    if(video){const play=place(button(s,'播放预览','▷ 预览','Primary'),908,328);link(play,'preview');}
    const actions=place(layout(s,'结果操作'),835,565);
    const preview=button(actions,'查看','查看');link(preview,'preview');
    const download=button(actions,'下载','下载');link(download,'download');
    const again=button(actions,'继续',video?'再次编辑':'用作视频参考');link(again,video?'videoReady':'videoReady');
    const done=place(text(s,'结果提示','示例结果 · 用于验证布局与操作流程',12,'muted'),835,618);
    return result;
  }

  // A finite-state prototype. Every named target must exist before reactions are written.
  let s=shell('empty','00 · 空画布',0);
  let w=place(layout(s,'欢迎',true,14),490,327);
  text(w,'标题','从一份素材，开始一个镜头',24);
  text(w,'副标题','上传角色图，或新建图片与视频生成节点',14,'muted');
  link(button(w,'开始上传','上传第一份素材','Primary'),'upload');

  s=shell('upload','01 · 上传资源',1);dim(s,.45);
  let modal=frame(s,'上传资源面板',450,235,540,380,'panel',16);
  place(text(modal,'标题','上传资源',22),28,24);
  place(text(modal,'说明','将图片或视频添加到画布',14,'muted'),28,68);
  let drop=frame(modal,'上传区域',28,118,484,135,'raised',10);drop.strokes=fill('border');drop.dashPattern=[6,6];
  place(text(drop,'提示','拖入文件，或点击选择文件',16),115,36);
  place(text(drop,'示例说明','原型使用内置角色图模拟上传',12,'muted'),140,70);
  link(drop,'assets');
  link(place(button(modal,'选择示例','选择示例图片','Primary'),28,284),'assets');
  link(place(button(modal,'取消','取消'),208,284),'empty');
  link(place(button(modal,'查看失败','查看失败示例','Quiet'),330,284),'uploadFail');

  s=shell('assets','02 · 素材已上传',2);let a=assets(s);link(a.male,'preview');link(a.female,'preview');
  place(text(s,'引导','点击底部「＋ 图片」，创建第一个生成节点',14,'muted'),500,714);

  s=shell('edit','03 · 图片节点编辑',3);assets(s);target(s);composer(s,'edit');note(s,'从「＋参考」开始，选择角色素材');
  s=shell('select','04 · 选择参考',4);a=assets(s,true);target(s);composer(s,'select');
  controls.select.composer.opacity=.4;
  const banner=place(layout(s,'参考选择提示'),463,82);padding(banner,12);banner.fills=fill('accentSurface');banner.cornerRadius=12;
  text(banner,'提示','选择画布素材作为参考',14);link(button(banner,'返回','返回节点'),'edit');link(button(banner,'关闭','×'),'edit');
  link(a.male,'referenced');link(a.female,'femaleRef');note(s,'点击角色图片添加参考；Esc 结束选择');

  s=shell('referenced','05 · 已添加参考',5);line(s,[395,313],[870,310]);assets(s);target(s);composer(s,'referenced',{ref:true});note(s,'点击提示词区域，查看 @ 引用菜单');
  s=shell('mention','06 · @ 引用菜单',6);line(s,[395,313],[870,310]);assets(s);target(s);composer(s,'mention',{ref:true});
  let menu=place(layout(s,'引用菜单',true,6),733,556);padding(menu,12);menu.fills=fill('raised');menu.cornerRadius=12;menu.effects=shadow;menu.counterAxisAlignItems='MIN';
  const search=button(menu,'搜索','⌕ 搜索素材名称');link(search,'search');
  text(menu,'分组','已引用',12,'muted');
  let item=layout(menu,'已引用素材');crop(item,'缩略图',0,0,28,28,'male');link(button(item,'引用角色','陈志远_角色三视图.png','Quiet'),'ready');link(item,'ready');
  text(menu,'其他素材','素材引用',12,'muted');
  for(const [label,to] of [['图片  ›','imageRefs'],['视频  ›','videoRefs']])link(button(menu,label,label,'Quiet'),to);

  s=shell('ready','07 · 提示词就绪',7);line(s,[395,313],[870,310]);assets(s);target(s);composer(s,'ready',{ref:true,ready:true});note(s,'引用按名称显示；点击「生成」体验任务状态');
  s=shell('expanded','08 · 放大编辑',8);assets(s);target(s);dim(s,.6);composer(s,'expanded',{ref:true,ready:true,expanded:true});
  s=shell('working','09 · 图片生成中',9);line(s,[395,313],[870,310]);assets(s);target(s,'working');composer(s,'working',{ref:true,ready:true,working:true});note(s,'任务已提交，当前提示词与参考已保存');
  link(s,'success',{type:'AFTER_TIMEOUT',timeout:1.8});
  const failDemo=place(button(s,'失败演示','查看失败状态','Quiet'),1180,738);link(failDemo,'failed');

  s=shell('success','10 · 图片生成完成',10);line(s,[395,313],[835,350]);assets(s);resultNode(s);
  s=shell('video','11 · 视频节点编辑',11);assets(s);target(s,'empty',true);composer(s,'video',{video:true});note(s,'点击「＋参考」选择已有的图片结果');
  s=shell('videoReady','12 · 图片作为视频参考',12);assets(s);const r=crop(s,'图片结果',590,165,145,258,'result');line(s,[735,295],[870,310]);
  place(text(s,'图片结果名称','图片节点 1',13,'muted'),590,138);target(s,'empty',true);composer(s,'videoReady',{video:true,ref:true,ready:true});
  s=shell('videoWorking','13 · 视频生成中',13);assets(s);target(s,'working',true);composer(s,'videoWorking',{video:true,ref:true,ready:true,working:true});link(s,'videoSuccess',{type:'AFTER_TIMEOUT',timeout:1.8});
  s=shell('videoSuccess','14 · 视频生成完成',14);assets(s);resultNode(s,true);

  s=shell('failed','15 · 生成失败',15);assets(s);target(s,'failed');composer(s,'failed',{ref:true,ready:true});
  const error=place(layout(s,'失败提示',true,8),720,90);padding(error,16);error.fills=fill('panel');error.cornerRadius=12;
  text(error,'原因','本次生成未完成',16,'danger');text(error,'恢复','提示词和参考已保留，可修改后重新提交。',13,'muted');
  link(button(error,'重试','重新生成'),'working');

  // Supplementary overlays are full frames to keep transitions inspectable and deterministic.
  function info(key,title,index,body,actions){
    const root=shell(key,title,index);dim(root,.55);const m=frame(root,'说明面板',420,220,600,440,'panel',16);
    place(text(m,'标题',title.replace(/^\d+ · /,''),22),28,24);
    place(text(m,'正文',body,15,'muted',544),28,82);
    const row=place(layout(m,'操作'),28,350);
    for(const [label,to] of actions)link(button(row,label,label),to);return root;
  }
  info('uploadFail','16 · 上传失败',16,'文件未能上传，画布中的其他内容不受影响。\n\n可重新选择文件，或返回画布。\n原型演示错误反馈，不会上传真实文件。',[['重新选择','upload'],['返回画布','assets']]);
  info('parameters','17 · 生成参数',17,'第一版提供模型、比例、图片数量或视频时长选择。\n\n这里展示的是参数位置与入口。实际选项、参考格式与计费，需要在模型能力验证后确定。',[['返回图片编辑','ready'],['返回视频编辑','videoReady']]);
  info('canvasHelp','18 · 画布操作',18,'正式产品：空格拖动画布、滚轮缩放、拖动节点调整位置。\n\n本 Figma 原型演示固定场景与点击跳转，不能模拟真正的无限画布。',[['返回素材画布','assets'],['继续编辑','ready']]);
  info('download','19 · 下载结果',19,'正式产品在这里下载生成文件。\n\n此原型只使用截图中的示例素材，不产生真实图片、视频或下载文件。',[['返回图片结果','success'],['返回视频结果','videoSuccess']]);
  info('femaleRef','20 · 另一个参考素材',20,'你选择了虞诗音_角色三视图.png。\n\n主流程演示使用陈志远素材；正式产品支持选择任意符合模型要求的素材，并按文件名引用。',[['继续示例流程','referenced'],['返回选择','select']]);
  info('search','21 · 搜索引用',21,'搜索示例：陈志远\n\n匹配结果：陈志远_角色三视图.png\n\n原型使用预设输入演示搜索结果，暂不支持自由键入。',[['插入引用','ready'],['返回菜单','mention']]);
  info('imageRefs','22 · 图片素材列表',22,'可用图片\n\n陈志远_角色三视图.png\n虞诗音_角色三视图.png\n图片节点 1（已生成结果）',[['引用陈志远','ready'],['用结果生成视频','videoReady'],['返回菜单','mention']]);
  info('videoRefs','23 · 参考素材选择',23,'本演示已准备：图片节点 1。\n\n点击选择后，将其加入视频节点的参考区。\n视频参考输入将在实际模型支持时开放。',[['选择图片节点 1','videoReady'],['返回视频节点','video']]);

  s=shell('preview','24 · 素材预览',24);dim(s,.7);crop(s,'放大素材',568,112,304,540,'result');
  place(text(s,'示例标记','示例画面 · 预览布局',13,'muted'),568,672);
  link(place(button(s,'关闭预览','关闭预览'),665,730),'success');
  link(s,'success',{type:'ON_KEY_DOWN',device:'KEYBOARD',keyCodes:[27]});

  // A one-page brief and explicit demo boundaries live inside the same Figma page.
  const guide=frame(page,'开始这里 · V1 说明与导航',-1500,200,1440,900,'canvas',0);screens.guide=guide;
  place(text(guide,'眉题','帧间 / AI 漫剧工作台',16,'accent'),72,60);
  place(text(guide,'标题','把素材变成镜头',44),72,112);
  place(text(guide,'副标题','第一版桌面端交互原型 · 1440 × 900',18,'muted'),72,190);
  place(text(guide,'主流程','上传资源 → 图片节点 → 选择参考 → @ 文件名 → 生成图片 → 作为视频参考',18,'text',1260),72,258);
  place(text(guide,'规则','已确认的规则\n默认尺寸编辑区，右上角放大 / 恢复。\n参考缩略图数字表示顺序；引用显示文件名或生成节点名。\n选择参考时保留正在编辑的节点。\n点击生成才发起任务，连线不会自动执行。',16,'muted',620),72,340);
  place(text(guide,'范围','演示边界\n点击可跳转；@ 输入与搜索使用预设状态。\n上传、模型生成、视频播放、下载均为模拟。\n模型参数、价格、账号权限和实际保存尚待开发。\n素材摘自用户提供的 LibTV 截图，仅用于本次设计演示。',16,'muted',620),760,340);
  const nav=place(layout(guide,'原型入口'),72,680);
  link(button(nav,'开始','从上传开始','Primary'),'empty');link(button(nav,'编辑','直接体验编辑'),'ready');link(button(nav,'错误','查看失败状态'),'failed');
  place(text(guide,'验收','检查：输入框放大后内容保留；引用名称清晰；生成失败后输入仍在；图片结果能继续用于视频。',14,'muted',1270),72,772);

  for(const {node,target,trigger} of links){
    if(!screens[target])throw new Error('Missing prototype destination: '+target);
    const reaction={trigger,actions:[{type:'NODE',destinationId:screens[target].id,navigation:'NAVIGATE',transition:{type:'DISSOLVE',easing:{type:'EASE_OUT'},duration:.12},preserveScrollPosition:false}]};
    const previous=node.reactions?Array.from(node.reactions):[];
    await node.setReactionsAsync(previous.concat(reaction));
  }
  for(const key of ['select','mention','expanded']) {
    const dest=key==='select'?'edit':key==='mention'?'referenced':'ready';
    await screens[key].setReactionsAsync([{trigger:{type:'ON_KEY_DOWN',device:'KEYBOARD',keyCodes:[27]},actions:[{type:'NODE',destinationId:screens[dest].id,navigation:'NAVIGATE',transition:null,preserveScrollPosition:false}]}]);
  }
  page.flowStartingPoints=[{nodeId:guide.id,name:'V1 · 从这里开始'},{nodeId:screens.ready.id,name:'快速体验 · 节点编辑'}];
  page.prototypeBackgrounds=fill('canvas');page.name=PAGE;
  figma.viewport.scrollAndZoomIntoView([guide]);
  figma.currentPage.selection=[guide];
  figma.closePlugin('原型已创建：26 个页面状态，含组件、说明和点击连接。点击右上角演示按钮体验。');
}
main().catch(error => {
  console.error(error);
  figma.closePlugin('导入未完成：'+error.message+'。原有页面没有被覆盖；可删除新增的「构建中」页面后重试。');
});
