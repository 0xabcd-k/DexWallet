/* Offline product review. All operations show documentation, never send requests. */
(() => {
  'use strict';
  const pages = window.PAGES;
  const byId = new Map(pages.map(page => [page.id, page]));
  const $ = selector => document.querySelector(selector);
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const plain = value => String(value).replace(/\[([^\]]+)\]\([^)]+\)/g, '$1');
  let current = pages[0], tab = 'layout', priorFocus;
  const dialog = $('#interaction');
  const modalSamples = {
    wallet: {title:'选择钱包', fields:[['当前钱包','Tonkeeper · TON · 已连接'],['其他钱包','我的内置钱包 · TON · 已备份']], buttons:['连接新钱包','管理钱包'], after:'选择后切换当前钱包。新增连接前往兼容钱包目录；管理入口进入钱包管理页。此处只切换链上钱包，不改变美元账户身份。'},
    transfer: {title:'确认转账', fields:[['付款来源','Tonkeeper · TON · USDT'],['收款地址','UQ…示例地址（正式界面显示完整地址）'],['转账数量','10.00 USDT'],['网络费','实时估算 · 由付款钱包承担']], buttons:['返回修改','确认转账'], after:'确认后进行本机验证或同设备外部钱包授权。已广播但结果未知时进入账单查询，不再次发送。'},
    invite: {title:'邀请好友完成此任务', fields:[['当前任务','体验指定功能'],['链接','此任务专属邀请链接（占位）'],['参与条件','本人已完成首次任务']], buttons:['复制链接','分享任务'], after:'仅已完成首个任务的用户可见。好友首次完成时绑定关系；自己打开自己的链接返回任务页。'},
    slot: {title:'本次奖励', fields:[['已消耗','100 积分'],['示例结果','$ 0.09'],['到账位置','待提现奖励']], buttons:['查看记录','完成'], after:'这里只展示固定结果。实际扣分、结果与奖励入账在一笔业务中完成，动画结束不负责发奖。'},
    order: {title:'输入订单号', fields:[['平台订单号','ORDER-DEMO']], buttons:['取消','查询'], after:'查询到有效未付款订单进入支付确认；已付款进入结果页；未找到显示错误。'},
    payment: {title:'确认支付组成', fields:[['收款方','Demo 商户'],['订单金额','$ 10.00'],['美元余额','$ 3.00'],['链上资产','TON / USDT · 7.00 USDT'],['网络费','按当前有效报价单列']], buttons:['返回修改','确认付款'], after:'先预留美元部分，再唤起钱包授权链上部分。钱包返回不等于付款成功；进入支付结果页核验。'},
    refund: {title:'申请整单退款', fields:[['原订单','O-DEMO-01'],['退款金额','$ 10.00'],['原资金来源','$ 3 美元余额 + 7 USDT（TON）'],['申请原因','填写退款原因']], buttons:['取消','提交申请'], after:'商户提交后交平台审核。按原资金来源分别退款；处于退款中或结果未知时不重复申请。'},
    createOrder: {title:'确认创建收款单', fields:[['订单标题','商品 A'],['金额','$ 10.00'],['有效期','15 分钟'],['商户订单号','MERCHANT-DEMO-01']], buttons:['返回修改','确认创建'], after:'校验后生成唯一订单，原页展示支付链接与商户订单二维码。相同商户订单号重复提交返回已有记录。'},
    settlement: {title:'申请商户结算', fields:[['申请金额','100.00 USD'],['到账网络 / 币种','TON / USDT'],['收款地址','填写完整链上地址'],['费用与净到账','按有效报价计算']], buttons:['取消','确认申请'], after:'提交后占用可结算余额并进入审核。最低 10 美元为规划参数；报价过期须重新确认。'},
    review: {title:'审核申请详情', fields:[['申请人 / 类型','Lin · 奖励提现'],['申请金额 / 到账目标','$ 3.20 → 本人美元余额'],['资金占用','已从待提现奖励扣除 $ 3.20'],['条件与风险','Pro 已解锁 · 命中人工复核条件'],['处理说明','填写判断依据']], buttons:['要求补充说明','拒绝','通过'], after:'通过后进入实际执行，不直接写已到账。拒绝需填写用户可见原因，并退回本例的待提现奖励账户。'},
    user: {title:'用户详情', fields:[['用户','Lin · DW-1024'],['美元余额','$ 128.00'],['链上资产','由钱包按网络查询'],['任务积分 / 待提现奖励','800 积分 / $ 1.60'],['Pro 进度','1,800 / 2,000'],['邀请关系','只读 · 已绑定']], buttons:['查看账单','查看审核记录'], after:'此处不提供任意修改余额、撤销任务奖励或更改上级的入口。交易限制另填原因并确认。'}
  };
  const clean = text => text.replace(/[\s／/（）()]/g, '');
  function findAction(label) {
    return current.actions.find(a => a.label === label) || current.actions.find(a => a.label.split(/[／/]/).some(part => clean(part) === clean(label))) || current.actions.find(a => clean(a.label).includes(clean(label)));
  }
  function nav() {
    const query = $('#search').value.trim().toLowerCase();
    const filtered = pages.filter(p => (p.title + p.group + p.role + p.display.join('') + p.actions.map(a=>a.label).join('')).toLowerCase().includes(query));
    $('#count').textContent = `${filtered.length} / ${pages.length}`;
    let role='', group='';
    $('#navigation').innerHTML = filtered.map(p => {
      let head='';
      if (p.role !== role) { role=p.role; group=''; head+=`<div class="role-label">${esc(role)}</div>`; }
      if (p.group !== group && p.role === '用户端') { group=p.group; head+=`<div class="nav-group">${esc(group)}</div>`; }
      return head + `<a class="nav-item ${p.id===current.id?'active':''}" ${p.id===current.id?'aria-current="page"':''} href="#${p.id}"><span>${p.id.toUpperCase()}</span>${esc(p.title)}</a>`;
    }).join('') || '<p class="empty">没有匹配页面，试试“转账”或“提现”。</p>';
  }
  function fields(items) {
    return `<div class="fields">${items.map(([label,value])=>`<div class="field"><label>${esc(label)}</label><div class="field-value">${esc(value)}</div></div>`).join('')}</div>`;
  }
  function regionContent(r) {
    const items=r.items;
    switch(r.kind) {
      case 'stats':return `<div class="stats">${items.map(([label,value])=>`<div class="stat"><label>${esc(label)}</label><strong>${esc(value)}</strong></div>`).join('')}</div>`;
      case 'fields':return fields(items);
      case 'rows':return `<div class="rows">${items.map(([title,sub,value,target])=>{const modal=target?.startsWith('modal:');return `${modal?`<button data-modal="${esc(target.slice(6))}"`:target?`<a href="#${esc(target)}"`: '<div'} class="row"><span><strong>${esc(title)}</strong><small>${esc(sub)}</small></span><span class="row-value">${esc(value)}${target?' ›':''}</span>${modal?'</button>':target?'</a>':'</div>'}`;}).join('')}</div>`;
      case 'tabs':return `<div class="tabs" aria-label="标签布局示意">${items.map(t=>`<span>${esc(t)}</span>`).join('')}</div>`;
      case 'steps':return `<ol class="steps">${items.map(t=>`<li>${esc(t)}</li>`).join('')}</ol>`;
      case 'text':return `<div class="text-block">${items.map(t=>`<p>${esc(t)}</p>`).join('')}</div>`;
      case 'qr':return `<div class="qr"><div class="qr-placeholder"><span>收款码占位<br>不可扫描</span></div>${items.map(t=>`<span>${esc(t)}</span>`).join('')}</div>`;
      case 'scan':return `<div class="scan">${esc(items[0])}</div>`;
      case 'slots':return `<div class="slots">${items.map(t=>`<span>${esc(t)}</span>`).join('')}</div>`;
      case 'progress':return `<div class="progress"><strong>${esc(items[0])}</strong><div class="progress-track"><span style="width:${Number(items[1])}%"></span></div></div>`;
      case 'status':return `<div class="status"><strong>${esc(items[0])}</strong><small>${esc(items[1])}</small></div>`;
      case 'table':return `<div class="table-scroll"><table><thead><tr>${items[0].map(t=>`<th scope="col">${esc(t)}</th>`).join('')}</tr></thead><tbody>${items.slice(1).map(row=>`<tr>${row.map((t,i)=>`<td>${i===0 && current.id==='m02'?`<a class="table-link" href="#m03">${esc(t)}</a>`:esc(t)}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`;
      default:throw new Error(`Unsupported region: ${r.kind}`);
    }
  }
  function draw() {
    const mobile = current.role==='用户端';
    const primary=current.controls.find(label=>/^(确认|提交|下一步|去完成|完成并|100 积分|登录|使用 Telegram)/.test(label))||current.controls[0];
    const controls = `<div class="controls">${current.controls.map(label=>`<button class="${label===primary?'primary':''}" data-control="${esc(label)}">${esc(label)}</button>`).join('')}</div>`;
    const content = current.regions.map((r,i)=>`<section class="region" id="region-${i}"><div class="region-title"><button class="marker" data-region="${i}" aria-label="查看区域 ${i+1} 说明">${String(i+1).padStart(2,'0')}</button><h3>${esc(r.title)}</h3></div>${regionContent(r)}${current.controlsAfter===i?controls:''}</section>`).join('');
    const sidebar = !mobile && current.role!=='公共' ? `<nav class="app-sidebar" aria-label="产品后台导航">${pages.filter(p=>p.role===current.role && !['m03'].includes(p.id)).map(p=>`<a href="#${p.id}" class="${p.id===current.id?'active':''}">${esc(p.title.replace(/^(平台|商户)/,''))}</a>`).join('')}</nav>`:'';
    const bottom = mobile && current.id!=='a01' ? `<nav class="bottom-nav" aria-label="产品底部导航">${[['w01','钱包'],['t01','任务'],['i01','邀请']].map(([id,title])=>`<a class="${id===current.id?'active':''}" href="#${id}">${title}</a>`).join('')}</nav>`:'';
    $('#device').innerHTML = `<div class="device ${mobile?'mobile':'desktop'}"><div class="device-bar"><span>${mobile?'9:41 · DexWallet':current.role+' / DexWallet'}</span><span>示例场景</span></div><div class="device-title">${esc(current.title)}<span>${mobile?'移动端':'后台布局'}</span></div><div class="${sidebar?'desktop-shell':''}">${sidebar}<div class="device-content">${content}${current.controlsAfter===undefined?controls:''}</div></div>${bottom}</div>`;
  }
  function links(action) {return action.targets.map(t=>`<a class="notes-link" href="#${esc(t.id)}">${esc(t.title)} →</a>`).join('');}
  function noteContent(type) {
    if(type==='layout') return current.regions.map((r,i)=>`<div class="note"><div class="note-title"><button class="marker" data-highlight="${i}">${String(i+1).padStart(2,'0')}</button>${esc(r.title)}</div><p>${esc(r.note)}</p></div>`).join('') + `<details><summary>展开原文的完整展示要求</summary><ul class="note-list">${current.display.map(t=>`<li>${esc(plain(t))}</li>`).join('')}</ul></details>`;
    if(type==='actions') return '<div class="notes-callout">图上展示当前示例状态的主要按钮；下面包含该页全部操作。点击可看行为与目标页面。</div>' + current.actions.map((a,i)=>`<div class="note"><button class="action-note" data-action="${i}">${String(i+1).padStart(2,'0')} · ${esc(a.label)}</button><p>${esc(plain(a.description))}</p>${links(a)}</div>`).join('');
    if(type==='dialogs') return (current.modal?`<div class="note"><button data-modal="${current.modal}">预览：${esc(modalSamples[current.modal].title)} ↗</button></div>`:'') + current.dialogs.map((t,i)=>`<div class="note"><div class="note-title">弹窗 / 抽屉 ${i+1}</div><p>${esc(plain(t))}</p></div>`).join('') + '<div class="notes-callout">预览用于核对内容与确认动作。未提供图形预览的弹窗先按文字说明审阅。</div>';
    return '<div class="notes-callout">这里列出必须覆盖的状态和边界；当前线框图展示其中一个示例场景，不模拟真实交易。</div>'+current.states.map((t,i)=>`<div class="note"><div class="note-title">状态 ${String(i+1).padStart(2,'0')}</div><p>${esc(plain(t))}</p></div>`).join('');
  }
  function notes() {
    document.querySelectorAll('[data-tab]').forEach(b=>{b.setAttribute('aria-selected',String(b.dataset.tab===tab));b.tabIndex=b.dataset.tab===tab?0:-1;});
    $('#notes').setAttribute('aria-labelledby',`tab-${tab}`);
    $('#notes').innerHTML=noteContent(tab);
  }
  function openDialog(title,html) {
    priorFocus=document.activeElement;
    $('#dialog-title').textContent=title;
    $('#dialog-body').innerHTML=html;
    if(!dialog.open) dialog.showModal();
  }
  function sample(key) {
    const s=modalSamples[key];
    openDialog(s.title,`<div class="dialog-example">${fields(s.fields)}<div class="sample-footer">${s.buttons.map(t=>`<button data-sample-next="${esc(key)}">${esc(t)}</button>`).join('')}</div></div><p class="dialog-description">${esc(s.after)}</p><p class="dialog-caption">按钮只展示处理说明，不提交申请或触发钱包。输入区域为版式占位。</p>`);
  }
  function action(index) {
    const a=current.actions[index];
    openDialog(a.label,`<p class="dialog-description">${esc(plain(a.description))}</p>${current.modal?`<button data-modal="${current.modal}">查看本页典型弹窗：${esc(modalSamples[current.modal].title)}</button>`:''}<div class="dialog-actions">${a.targets.map(t=>`<a class="primary" href="#${esc(t.id)}">进入${esc(t.title)} →</a>`).join('')}<button data-close>返回页面</button></div>`);
  }
  function render() {
    if(dialog.open) dialog.close();
    current=byId.get(location.hash.slice(1)) || pages[0];
    document.title=`${current.title} · DexWallet 图文规划`;
    $('#title').textContent=current.title;
    $('#breadcrumb').textContent=`${current.role} / ${current.group} / ${current.id.toUpperCase()}`;
    $('#intro').textContent=plain(current.intro);
    $('#source').href='../'+current.source;
    $('#position').textContent=`${pages.indexOf(current)+1} / ${pages.length} 页`;
    $('#previous').disabled=pages.indexOf(current)===0;
    $('#next').disabled=pages.indexOf(current)===pages.length-1;
    nav();draw();notes();
  }
  document.addEventListener('click',event=>{
    const el=event.target.closest('button,a');
    if(!el)return;
    if(el.dataset.tab){tab=el.dataset.tab;notes();}
    if(el.dataset.region!==undefined){tab='layout';notes();document.querySelectorAll('.note')[Number(el.dataset.region)]?.scrollIntoView({block:'nearest',behavior:'smooth'});}
    if(el.dataset.highlight!==undefined){document.querySelectorAll('.region').forEach(r=>r.classList.remove('highlight'));const r=$(`#region-${el.dataset.highlight}`);r.classList.add('highlight');r.scrollIntoView({block:'center',behavior:'smooth'});}
    if(el.dataset.action!==undefined)action(Number(el.dataset.action));
    if(el.dataset.control){const a=findAction(el.dataset.control);if(a)action(current.actions.indexOf(a));else openDialog(el.dataset.control,`<p class="dialog-description">${esc(current.intro)}</p><p class="dialog-description">此入口的完整操作见右侧“按钮”说明。</p><div class="dialog-actions"><button data-close>返回页面</button></div>`);}
    if(el.dataset.modal)sample(el.dataset.modal);
    if(el.dataset.sampleNext){const s=modalSamples[el.dataset.sampleNext];openDialog(`${el.textContent} · 行为说明`,`<p class="dialog-description">${esc(/取消|返回修改/.test(el.textContent)?'关闭弹窗并返回原表单，保留原内容。':s.after)}</p><p class="dialog-caption">这是交互说明，未执行实际操作。</p><div class="dialog-actions"><button data-modal="${esc(el.dataset.sampleNext)}">返回弹窗预览</button><button data-close>关闭预览</button></div>`);}
    if(el.hasAttribute('data-close'))dialog.close();
    if(el.tagName==='A' && el.getAttribute('href')?.startsWith('#') && dialog.open)dialog.close();
  });
  $('.review-tabs').addEventListener('keydown',e=>{if(!['ArrowRight','ArrowLeft'].includes(e.key))return;const types=['layout','actions','dialogs','states'];tab=types[(types.indexOf(tab)+(e.key==='ArrowRight'?1:3))%4];notes();$(`#tab-${tab}`).focus();e.preventDefault();});
  $('#close-dialog').addEventListener('click',()=>dialog.close());
  dialog.addEventListener('click',e=>{if(e.target===dialog){const r=dialog.getBoundingClientRect();if(e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom)dialog.close();}});
  dialog.addEventListener('close',()=>{if(priorFocus?.isConnected)priorFocus.focus();});
  $('#search').addEventListener('input',nav);
  $('#previous').addEventListener('click',()=>{location.hash=pages[pages.indexOf(current)-1].id;});
  $('#next').addEventListener('click',()=>{location.hash=pages[pages.indexOf(current)+1].id;});
  $('#print').addEventListener('click',()=>window.print());
  window.addEventListener('beforeprint',()=>{$('#notes').innerHTML=['layout','actions','dialogs','states'].map(type=>`<h2>${{layout:'布局说明',actions:'按钮行为',dialogs:'弹窗与抽屉',states:'状态与边界'}[type]}</h2>${noteContent(type)}`).join('');});
  window.addEventListener('afterprint',notes);
  window.addEventListener('hashchange',render);
  render();
})();
