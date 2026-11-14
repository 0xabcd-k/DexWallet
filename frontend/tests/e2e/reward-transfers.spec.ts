import { test, expect, type Page } from '@playwright/test';
import { envelope, mockAccount, seedSession, userIdx } from './auth-fixture';
const base='http://127.0.0.1:5173';
const original={idx:'03333333333333333',userIdx,amount:'2.00',fee:'0.00',status:'pending_review',reviewReason:'',createdAt:'2026-11-15T00:00:00Z',resolvedAt:''};
async function fixture(page:Page,pro=true){
 await mockAccount(page);await seedSession(page);
 const state={posts:0,key:'',result:{...original}};
 await page.route('**/api/v1/user/reward-transfer-state',route=>route.fulfill({json:envelope({available:true,pro,minimumAmount:'2.00',rewardUsd:state.posts?'1.00':'3.00',pendingUsd:state.posts&&state.result.status==='pending_review'?'2.00':'0.00',fee:'0.00'})}));
 await page.route('**/api/v1/user/reward-transfers',route=>{state.posts++;state.key=route.request().postDataJSON().requestKey;return route.fulfill({json:envelope(state.result)});});
 await page.route('**/api/v1/user/reward-transfers?*',route=>route.fulfill({json:envelope({items:[state.result],nextCursor:''})}));
 await page.route(`**/api/v1/user/reward-transfers/${original.idx}`,route=>route.fulfill({json:envelope(state.result)}));
 return state;
}
async function submit(page:Page){await page.getByRole('textbox',{name:'Amount (USD)',exact:true}).fill('2.00');await page.getByRole('button',{name:'Confirm transfer',exact:true}).click();await page.getByRole('dialog').getByRole('button',{name:'Confirm transfer',exact:true}).click();}
test.use({viewport:{width:390,height:844}});
test('risk review is shown independently from eligibility, then rejection returns rewards',async({page})=>{
 const s=await fixture(page);await page.goto(`${base}/#/reward-transfers`);await submit(page);
 await expect(page.getByRole('heading',{name:'Awaiting manual review'})).toBeVisible();expect(s.posts).toBe(1);
 await expect(page.getByText('Pro is required to apply.',{exact:true})).toHaveCount(0);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await expect(page.getByRole('dialog')).toBeHidden();
 await page.screenshot({path:`test-results/reward-review-${test.info().project.name}.png`,fullPage:true});
 s.result={...s.result,status:'rejected',reviewReason:'Risk review rejected',resolvedAt:'2026-11-15T00:01:00Z'};
 await page.getByRole('link',{name:'Application details',exact:true}).click();await expect(page.getByRole('heading',{name:'Rejected; rewards returned'})).toBeVisible();expect(s.posts).toBe(1);
});
test('automatic route credits USD after confirmation',async({page})=>{const s=await fixture(page);s.result={...s.result,status:'completed',resolvedAt:'2026-11-15T00:01:00Z'};await page.goto(`${base}/#/reward-transfers`);await submit(page);await expect(page.getByRole('heading',{name:'Transferred to USD balance'})).toBeVisible();expect(s.posts).toBe(1);});
test('business eligibility and insufficient rewards disable confirmation',async({page})=>{const s=await fixture(page,false);await page.goto(`${base}/#/reward-transfers`);await page.getByRole('textbox',{name:'Amount (USD)',exact:true}).fill('4');await expect(page.getByRole('button',{name:'Confirm transfer',exact:true})).toBeDisabled();await expect(page.getByText('Pro is required to apply.',{exact:true})).toBeVisible();await expect(page.getByText('Insufficient available rewards.',{exact:true})).toBeVisible();expect(s.posts).toBe(0);});
test('lost response retains amount and key across reload and lookup miss',async({page})=>{
 const s=await fixture(page);let visible=false;
 await page.route('**/api/v1/user/reward-transfers',route=>{s.posts++;s.key=route.request().postDataJSON().requestKey;return route.abort('failed');});
 await page.route('**/api/v1/user/reward-transfers/by-request/*',route=>{expect(route.request().url()).toContain(s.key);return route.fulfill(visible?{json:envelope(s.result)}:{status:404,json:{code:'NOT_FOUND',message:'Missing',traceId:'test'}});});
 await page.goto(`${base}/#/reward-transfers`);await submit(page);await expect(page.getByRole('button',{name:'Check original application'})).toBeVisible();await page.reload();
 await page.getByRole('button',{name:'Check original application'}).click();await expect(page.getByRole('button',{name:'Retry original application'})).toBeVisible();expect(s.posts).toBe(1);
 visible=true;await page.getByRole('button',{name:'Check original application'}).click();await expect(page.getByRole('heading',{name:'Awaiting manual review'})).toBeVisible();expect(s.posts).toBe(1);
});
test('admin sees risk evidence and recovers a lost review response',async({page})=>{
 const admin='http://127.0.0.1:5175';const exp=Math.floor(Date.now()/1000)+259200;
 const token=`e30.${Buffer.from(JSON.stringify({iss:'dexwallet_test',aud:['admin-user'],sub:userIdx,auth_version:1,iat:exp-259200,exp})).toString('base64url')}.test-signature`;
 await page.addInitScript(token=>localStorage.setItem('dexwallet:admin:session',JSON.stringify({generation:'admin-test',token})),token);
 const data={transfer:{...original},riskDecision:'review',riskVersion:'local_mock_v1',riskReason:'Low score fixture',reviewerIdx:''};let reviews=0;
 await page.route('**/api/v1/admin/**',route=>{const url=new URL(route.request().url());
  if(url.pathname.endsWith('/current'))return route.fulfill({json:envelope({idx:userIdx,email:'admin@example.com',status:'active',mustChangePassword:false})});
  if(url.pathname.endsWith('/review')){reviews++;data.transfer={...data.transfer,status:'completed',reviewReason:'Reviewed',resolvedAt:'2026-11-15T00:01:00Z'};data.reviewerIdx=userIdx;return route.abort('failed');}
  if(url.pathname.endsWith(`/${original.idx}`))return route.fulfill({json:envelope(data)});
  return route.fulfill({json:envelope({items:[data.transfer],nextCursor:''})});
 });
 await page.goto(`${admin}/reward-transfers`);await page.getByRole('button',{name:'Open application'}).click();await expect(page.getByText('Low score fixture',{exact:false})).toBeVisible();
 await page.getByRole('textbox').fill('Reviewed');await page.getByRole('button',{name:'Approve and credit USD'}).click();await expect(page.getByRole('button',{name:'Retry original decision'})).toBeVisible();
 await expect(page.getByRole('button',{name:'Reject and return rewards'})).toHaveCount(0);
 await page.getByRole('button',{name:'Refresh applications'}).last().click();await expect(page.getByRole('button',{name:'Retry original decision'})).toHaveCount(0);expect(reviews).toBe(1);
});

test('a refusal does not discard an older pending request key',async({page})=>{
 const s=await fixture(page);let calls=0;const keys:string[]=[];
 await page.route('**/api/v1/user/reward-transfers',route=>{calls++;keys.push(route.request().postDataJSON().requestKey);return calls===1?route.abort('failed'):route.fulfill({status:422,json:{code:'INSUFFICIENT_BALANCE',message:'Insufficient',traceId:'test'}});});
 await page.route('**/api/v1/user/reward-transfers/by-request/*',route=>route.fulfill({status:404,json:{code:'NOT_FOUND',message:'Missing',traceId:'test'}}));
 await page.goto(`${base}/#/reward-transfers`);await submit(page);await page.getByRole('button',{name:'Retry original application'}).click();
 await expect(page.getByRole('button',{name:'Retry original application'})).toBeVisible();await page.reload();await page.getByRole('button',{name:'Check original application'}).click();
 await expect(page.getByRole('button',{name:'Retry original application'})).toBeVisible();expect(calls).toBe(2);expect(keys[0]).toBe(keys[1]);expect(s.posts).toBe(0);
});
