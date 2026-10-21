import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const config = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const frida = await import(pathToFileURL(config.fridaModule).href);
const source = fs.readFileSync(path.join(import.meta.dirname, 'agent.js'), 'utf8');
const statusPath = path.join(import.meta.dirname, 'layout-status.json');
const sessions = new Map();
const retryAfter = new Map();
let stopping = false;
let pollBusy = false;
const pause = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
function status(data) {
  try {
    const temp = statusPath + '.tmp';
    fs.writeFileSync(temp, JSON.stringify({ observed_at: new Date().toISOString(), helper_pid: process.pid, ...data }, null, 2));
    fs.renameSync(temp, statusPath);
  } catch { /* A diagnostic write must never affect the target application. */ }
}
async function shutdown() {
  if (stopping) return;
  stopping = true;
  clearInterval(timer);
  for (const { session, script } of sessions.values()) {
    try { await Promise.race([script.exports.restore(), pause(1500)]); } catch { }
    try { await script.unload(); } catch { }
    try { await session.detach(); } catch { }
  }
  sessions.clear();
  status({ state: 'stopped' });
  process.exit(0);
}
async function poll() {
  if (pollBusy || stopping) return;
  pollBusy = true;
  try {
    const device = await frida.getLocalDevice();
    const processes = await device.enumerateProcesses();
    const targets = processes.filter(target => target.name.toLowerCase() === 'kakaotalkui.exe');
    for (const target of targets) {
      if (sessions.has(target.pid) || stopping || Date.now() < (retryAfter.get(target.pid) ?? 0)) continue;
      let session;
      try {
        const details = await device.getProcessByPid(target.pid, { scope: frida.Scope.Full });
        const targetPath = details.parameters.path;
        if (typeof targetPath !== 'string' || path.basename(targetPath).toLowerCase() !== 'kakaotalkui.exe') {
          throw new Error('KakaoTalk executable path is unavailable');
        }
        session = await device.attach(target.pid);
        if (stopping) { await session.detach(); break; }
        const agentSource = `'use strict'; const AGENT_CONFIG = ${JSON.stringify({ expectedExecutable: targetPath })};\n${source}`;
        const script = await session.createScript(agentSource, { name: 'kakao-ad-layout' });
        script.message.connect(message => {
          if (message.type === 'send' || message.type === frida.MessageType.Send) {
            status({ target_pid: target.pid, ...message.payload });
          } else if (message.type === 'error' || message.type === frida.MessageType.Error) {
            status({ state: 'script-error', target_pid: target.pid, error: message.description });
            retryAfter.set(target.pid, Date.now() + 10000);
            if (sessions.get(target.pid)?.session === session) sessions.delete(target.pid);
            void script.unload().catch(() => {}).then(() => session.detach()).catch(() => {});
          }
        });
        session.detached.connect(reason => {
          if (sessions.get(target.pid)?.session === session) sessions.delete(target.pid);
          if (!stopping) status({ state: 'detached', target_pid: target.pid, reason });
        });
        sessions.set(target.pid, { session, script });
        await script.load();
      } catch (error) {
        sessions.delete(target.pid);
        if (session) try { await session.detach(); } catch { }
        status({ state: 'attach-error', target_pid: target.pid, error: String(error) });
      }
    }
    if (!targets.length) status({ state: 'waiting-for-kakaotalk' });
  } catch (error) {
    status({ state: 'poll-error', error: String(error) });
  } finally { pollBusy = false; }
}
const timer = setInterval(poll, 2000);
process.stdin.on('end', shutdown);
process.stdin.resume();
process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);
status({ state: 'starting' });
await poll();
