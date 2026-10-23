// Protocol/lifetime checks with a fake Frida device. Never attach to user apps.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { once } from 'node:events';

const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'kakao-helper-test-'));
const helper = path.resolve('internal/qt_layout/helper.mjs');
const events = path.join(directory, 'events.log');
let child;
const pause = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
async function until(predicate) {
  for (let count = 0; count < 100; count++) {
    if (predicate()) return;
    await pause(20);
  }
  throw new Error('Timed out waiting for helper');
}
try {
  fs.copyFileSync(helper, path.join(directory, 'helper.mjs'));
  fs.writeFileSync(path.join(directory, 'agent.js'), '// fake script source');
  fs.writeFileSync(path.join(directory, 'fake-frida.mjs'), `
    import fs from 'node:fs';
    const record = value => fs.appendFileSync(${JSON.stringify(events)}, value + '\\n');
    export const Scope = { Full: 'full' };
    export async function getLocalDevice() { return {
      async getProcessByPid(pid) { record('query:' + pid); return { parameters: { path: 'C:/fixture/KakaoTalkUI.exe' } }; },
      async attach(pid) { record('attach:' + pid); return {
        detached: { connect() {} },
        async detach() { record('detach:' + pid); },
        async createScript() { return {
          message: { connect() {} },
          async load() { record('load:' + pid); },
          async unload() { record('unload:' + pid); },
          exports: {
            async ensure() { record('ensure:' + pid); return { state: 'applied' }; },
            async restore() { record('restore:' + pid); }
          }
        }; }
      }; }
    }; }
  `);
  fs.writeFileSync(path.join(directory, 'config.json'), JSON.stringify({ fridaModule: path.join(directory, 'fake-frida.mjs') }));
  child = spawn(process.execPath, [path.join(directory, 'helper.mjs'), path.join(directory, 'config.json')], { windowsHide: true, stdio: ['pipe','ignore','inherit'] });
  const log = () => fs.existsSync(events) ? fs.readFileSync(events,'utf8') : '';
  await until(() => fs.existsSync(path.join(directory, 'layout-status.json')));
  await pause(150);
  assert.equal(log(), '', 'helper enumerated or attached before a target was supplied');
  child.stdin.write(JSON.stringify({ type: 'targets', pids: [123] }) + '\n');
  await until(() => log().includes('ensure:123'));
  await pause(200);
  const idleLog = log();
  const modified = fs.statSync(path.join(directory, 'layout-status.json')).mtimeMs;
  await pause(1200);
  assert.equal(log(), idleLog, 'applied helper kept polling');
  assert.equal(fs.statSync(path.join(directory, 'layout-status.json')).mtimeMs, modified, 'idle helper kept writing status');
  child.stdin.write(JSON.stringify({ type: 'targets', pids: [456] }) + '\n');
  await until(() => log().includes('ensure:456'));
  assert.match(log(), /restore:123\nunload:123\ndetach:123/);
  const exited = once(child, 'exit');
  child.stdin.end();
  const [code] = await exited;
  assert.equal(code, 0);
  assert.match(log(), /restore:456\nunload:456\ndetach:456/);
  console.log('Qt helper protocol, idle scheduling, target recreation and EOF cleanup passed');
} finally {
  if (child && child.exitCode === null) child.kill();
  assert.equal(path.dirname(path.resolve(directory)), path.resolve(os.tmpdir()));
  assert.ok(path.basename(directory).startsWith('kakao-helper-test-'));
  fs.rmSync(directory, { recursive: true, force: true });
}
