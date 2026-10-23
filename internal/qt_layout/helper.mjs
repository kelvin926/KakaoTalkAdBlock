import fs from 'node:fs';
import path from 'node:path';
import readline from 'node:readline';
import { pathToFileURL } from 'node:url';

const config = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const frida = await import(pathToFileURL(config.fridaModule).href);
const source = fs.readFileSync(path.join(import.meta.dirname, 'agent.js'), 'utf8');
const statusPath = path.join(import.meta.dirname, 'layout-status.json');
const sessions = new Map();
const retries = new Map();
let desired = new Set();
let stopping = false;
let busy = false;
let requested = false;
let timer;
let lastStatus = '';
const pause = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
function status(data) {
  const signature = JSON.stringify(data);
  if (signature === lastStatus) return;
  lastStatus = signature;
  try {
    const temp = statusPath + '.tmp';
    fs.writeFileSync(temp, JSON.stringify({ observed_at: new Date().toISOString(), helper_pid: process.pid, ...data }, null, 2));
    fs.renameSync(temp, statusPath);
  } catch { /* Diagnostics must never affect the target application. */ }
}
async function release({ session, script }) {
  try { await Promise.race([script.exports.restore(), pause(1500)]); } catch { }
  try { await script.unload(); } catch { }
  try { await session.detach(); } catch { }
}
async function shutdown() {
  if (stopping) return;
  stopping = true;
  clearTimeout(timer);
  // Wait for an in-flight attach so EOF cannot leave an orphaned session.
  while (busy) await pause(20);
  for (const entry of sessions.values()) await release(entry);
  sessions.clear();
  retries.clear();
  status({ state: 'stopped' });
  process.exit(0);
}
function schedule(delay = 100) {
  if (stopping) return;
  requested = true;
  if (busy || timer) return;
  timer = setTimeout(() => { timer = undefined; void reconcile(); }, delay);
}
async function reconcile() {
  if (stopping || busy) return;
  busy = true;
  requested = false;
  let needsInitialization = false;
  try {
    for (const [pid, entry] of sessions) {
      if (!desired.has(pid)) { sessions.delete(pid); await release(entry); }
    }
    for (const pid of retries.keys()) if (!desired.has(pid)) retries.delete(pid);
    const device = await frida.getLocalDevice();
    for (const pid of desired) {
      if (stopping) break;
      let entry = sessions.get(pid);
      const attempt = retries.get(pid) ?? { count: 0, after: 0 };
      if (Date.now() < attempt.after) continue;
      try {
        if (!entry) {
          const details = await device.getProcessByPid(pid, { scope: frida.Scope.Full });
          const targetPath = details.parameters.path;
          if (typeof targetPath !== 'string' || path.basename(targetPath).toLowerCase() !== 'kakaotalkui.exe') {
            throw new Error('Unexpected KakaoTalk executable path');
          }
          const session = await device.attach(pid);
          if (stopping) { await session.detach(); break; }
          const agentSource = `'use strict'; const AGENT_CONFIG = ${JSON.stringify({ expectedExecutable: targetPath })};\n${source}`;
          let script;
          try { script = await session.createScript(agentSource, { name: 'kakao-ad-layout' }); }
          catch (error) { await session.detach(); throw error; }
          entry = { session, script };
          sessions.set(pid, entry);
          script.message.connect(message => {
            if (message.type === 'send') status({ target_pid: pid, ...message.payload });
            else if (message.type === 'error') status({ state: 'script-error', target_pid: pid, error: message.description });
          });
          session.detached.connect(() => {
            if (sessions.get(pid)?.session === session) sessions.delete(pid);
          });
          await script.load();
        }
        const result = await Promise.race([entry.script.exports.ensure(), pause(1500).then(() => ({ state: 'waiting-for-gui' }))]);
        if (stopping) break;
        if (result.state === 'applied') {
          retries.delete(pid);
        } else {
          // Only bounded startup retries use a timer. Later checks come from
          // the parent's window events and 30-second health check.
          retries.set(pid, { count: attempt.count + 1, after: 0 });
          if (attempt.count < 9) needsInitialization = true;
        }
      } catch (error) {
        if (entry) { sessions.delete(pid); await release(entry); }
        retries.set(pid, { count: attempt.count + 1, after: Date.now() + 10000 });
        status({ state: 'attach-error', target_pid: pid, error: String(error) });
      }
    }
  } catch (error) {
    status({ state: 'helper-error', error: String(error) });
  } finally {
    busy = false;
    if (requested) schedule();
    else if (needsInitialization) schedule(1000);
  }
}
const input = readline.createInterface({ input: process.stdin });
input.on('line', line => {
  if (stopping || line.length > 65536) return;
  try {
    const message = JSON.parse(line);
    if (message.type === 'targets' && Array.isArray(message.pids)) {
      desired = new Set(message.pids.filter(pid => Number.isSafeInteger(pid) && pid > 0).slice(0, 32));
    } else if (message.type !== 'ensure') return;
    schedule();
  } catch { /* Ignore malformed control messages. */ }
});
input.on('close', shutdown);
process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);
status({ state: 'starting' });
