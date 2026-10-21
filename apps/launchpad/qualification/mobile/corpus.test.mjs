import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
const root = path.resolve(import.meta.dirname, '../../../../launchpad');
test('accessibility inventory exactly covers actual page routes and required journeys',()=>{
 const app = path.join(root,'client/apps/web/src/app');
 const pages=fs.readdirSync(app,{recursive:true}).filter(x=>x.endsWith('page.tsx')).map(x=>'/'+path.dirname(x)).map(x=>x==='/.'?'/':x).sort();
 const corpus=JSON.parse(fs.readFileSync(path.join(root,'client/qualification/accessibility/corpus.json')));
 assert.deepEqual(corpus.routes.map(x=>x.route).sort(),pages);
 assert.equal(Object.keys(corpus.journeys).length,10);
 for(const routes of Object.values(corpus.journeys))for(const route of routes)assert.ok(pages.includes(route));
 for(const row of corpus.routes){assert.equal(row.checks.length,6);assert.equal(row.status,'pending-browser-and-manual-AT');}
 assert.equal(corpus.observed,false);
});
test('responsive contract retains mandatory widths and continuity steps',()=>{
 const c=JSON.parse(fs.readFileSync(path.join(root,'client/qualification/responsive/corpus.json')));
 assert.deepEqual(c.widths,[320,375,390,768,1024,1440]);assert.deepEqual(c.zoom,[1,2]);assert.ok(c.steps.includes('rotate-preserve-pool-amount-scroll'));assert.equal(c.observed,false);
});
