// 真实后台回归：只读页面、深浅主题、桌面/手机；凭据只从进程环境读取。
import { createRequire } from 'node:module';
import { mkdirSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
const root = fileURLToPath(new URL('../../', import.meta.url));
const require = createRequire(path.join(root, 'web/package.json'));
const { chromium } = require('playwright-core');
const base = process.env.BASE_URL ?? 'http://127.0.0.1:8090';
const out = path.join(root, 'test-results/live-admin');
if (!process.env.LIVETAP_TEST_PASSWORD) throw new Error('需要已完成首次改密的 LIVETAP_TEST_PASSWORD，只通过环境变量传入');
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({ ...(process.env.CHROME_PATH ? {executablePath:process.env.CHROME_PATH} : {channel:'chrome'}), headless: true });
const results = [];
try {
  for (const [size, width, height] of [['desktop',1440,900],['mobile',390,844]]) {
    for (const theme of ['light','dark']) {
      const context = await browser.newContext({ viewport:{width,height},colorScheme:theme,reducedMotion:'reduce' });
      await context.addInitScript(t=>localStorage.setItem('livetap-theme',t),theme);
      const page = await context.newPage();
      const errors=[];
      page.on('pageerror',e=>errors.push(e.message));
      page.on('console',m=>{if(m.type()==='error')errors.push(m.text())});
      await page.goto(base,{waitUntil:'domcontentloaded'});
      await page.getByRole('heading',{name:'登录弹幕采集'}).waitFor();
      await page.screenshot({path:path.join(out,`${size}-${theme}-login.png`)});
      await page.locator('input[type=password]').fill(process.env.LIVETAP_TEST_PASSWORD);
      await page.getByRole('button',{name:'登录',exact:true}).click();
      await page.getByText('今日已采集消息',{exact:true}).waitFor();
      const response=await context.request.get(`${base}/admin/api/snapshot`);
      const snapshot=await response.json();
      const id=(snapshot.rooms.find(r=>r.status==='collecting'&&r.lastMsgAt)||snapshot.rooms[0])?.liveId;
      const routes=['overview','rooms','messages','settings',...(id?[`rooms?room=${id}`]:[])];
      for (const route of routes) {
        errors.length=0;
		if(route.includes('?')) {
			const events=await context.request.get(`${base}/admin/api/rooms/${id}/events`);
			if(!events.ok() || !Array.isArray(await events.json()))throw new Error('房间消息接口异常');
		}
        await page.goto(`${base}/#/${route}`,{waitUntil:'domcontentloaded'});
        await page.waitForTimeout(route.includes('?')?3000:600);
        const overflow=await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth);
        const file=`${size}-${theme}-${route.includes('?')?'room-detail':route}.png`;
        await page.screenshot({path:path.join(out,file),fullPage:true});
        results.push({file,route,horizontalOverflowPx:overflow,errors:[...errors]});
        console.log(`${file}: overflow=${overflow}, errors=${errors.length}`);
      }
      await context.close();
    }
  }
} finally { await browser.close(); }
writeFileSync(path.join(out,'ui-check.json'),JSON.stringify(results,null,2)+'\n');
if(results.some(r=>r.horizontalOverflowPx>0||r.errors.length))process.exitCode=1;
