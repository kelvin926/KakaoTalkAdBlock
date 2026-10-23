'use strict';
const nullPointer = ptr(0);

// All Qt reads and writes run in the target application's GUI thread.
// No conversation text, account state, or message models are inspected.
const normalizePath = path => path.replace(/\//g, '\\').toLowerCase();
if (normalizePath(Process.mainModule.path) !== normalizePath(AGENT_CONFIG.expectedExecutable)) {
  throw new Error('Unexpected target executable');
}
if (Process.arch !== 'x64' || Process.pointerSize !== 8) {
  throw new Error('The Qt layout adapter currently supports Windows x64 targets only');
}
const core = Process.getModuleByName('Qt6Core.dll');
const gui = Process.getModuleByName('Qt6Gui.dll');
const quick = Process.getModuleByName('Qt6Quick.dll');
const user = Process.getModuleByName('user32.dll');
const native = (module, name, result, args) => new NativeFunction(module.getExportByName(name), result, args);
const qtVersion = native(core, 'qVersion', 'pointer', [])().readUtf8String();
if (qtVersion !== '6.10.3') throw new Error('Unsupported Qt version: ' + qtVersion);

const className = native(core, '?className@QMetaObject@@QEBAPEBDXZ', 'pointer', ['pointer']);
const contentItem = native(quick, '?contentItem@QQuickWindow@@QEBAPEAVQQuickItem@@XZ', 'pointer', ['pointer']);
const winId = native(gui, '?winId@QWindow@@QEBA_KXZ', 'uint64', ['pointer']);
const height = native(quick, '?height@QQuickItem@@QEBANXZ', 'double', ['pointer']);
const width = native(quick, '?width@QQuickItem@@QEBANXZ', 'double', ['pointer']);
const visible = native(quick, '?isVisible@QQuickItem@@QEBA_NXZ', 'bool', ['pointer']);
const setVisible = native(quick, '?setVisible@QQuickItem@@QEAAX_N@Z', 'void', ['pointer', 'bool']);
const windowList = gui.getExportByName('?window_list@QGuiApplicationPrivate@@2V?$QList@PEAVQWindow@@@@A');
const metaFunctions = new Map();
function typeOf(object) {
  const address = object.readPointer().readPointer();
  let call = metaFunctions.get(address.toString());
  if (!call) {
    if (metaFunctions.size >= 64) metaFunctions.clear();
    call = new NativeFunction(address, 'pointer', ['pointer']);
    metaFunctions.set(address.toString(), call);
  }
  return className(call(object)).readUtf8String();
}

// Derive the QList field from this installed Qt build's own childItems getter.
// Refuse a different implementation instead of guessing a private ABI offset.
const getter = quick.getExportByName('?childItems@QQuickItem@@QEBA?AV?$QList@PEAVQQuickItem@@@@XZ');
const first = Instruction.parse(getter);
const second = Instruction.parse(first.next);
const input = first.operands[1];
const field = second.operands[1];
if (first.mnemonic !== 'mov' || first.operands[0].value !== 'r8' || input.type !== 'mem' || input.value.base !== 'rcx' || input.value.disp !== 8 || second.mnemonic !== 'mov' || field.type !== 'mem' || field.value.base !== 'r8' || field.value.disp < 0 || field.value.disp > 4096) {
  throw new Error('Unsupported QQuickItem child-list getter');
}
const childrenOffset = field.value.disp;
function listPointers(list) {
  const count = list.add(16).readS64().toNumber();
  if (count < 0 || count > 5000) throw new Error('Invalid Qt list size');
  const data = list.add(8).readPointer();
  const result = [];
  for (let i = 0; i < count; i++) result.push(data.add(i * 8).readPointer());
  return result;
}
const childrenOf = item => listPointers(item.add(8).readPointer().add(childrenOffset));

function findLayouts() {
  const result = [];
  for (const window of listPointers(windowList)) {
    if (!typeOf(window).startsWith('MainWindow_QMLTYPE_')) continue;
    const root = contentItem(window);
    const queue = [{ item: root, depth: 0 }];
    for (let n = 0; n < queue.length && n < 100; n++) {
      const { item, depth } = queue[n];
      const type = typeOf(item);
      const children = childrenOf(item);
      if (type === 'QQuickColumnLayout') {
        const stack = children.find(child => typeOf(child).startsWith('StackView_QMLTYPE_'));
        const separator = children.find(child => typeOf(child).startsWith('HorizontalSeperator_QMLTYPE_'));
        const loader = children.find(child => typeOf(child) === 'QQuickLoader' && childrenOf(child).some(banner => typeOf(banner).startsWith('AdBanner_QMLTYPE_')));
        if (stack && separator && loader && width(loader) >= width(root) * 0.8 && width(stack) >= width(root) * 0.8) {
          result.push({ window, root, stack, separator, loader });
          break;
        }
      }
      if (depth < 4) for (const child of children) queue.push({ item: child, depth: depth + 1 });
    }
  }
  return result;
}

const getPid = native(user, 'GetWindowThreadProcessId', 'uint', ['pointer', 'pointer']);
const getParent = native(user, 'GetParent', 'pointer', ['pointer']);
const getClass = native(user, 'GetClassNameW', 'int', ['pointer', 'pointer', 'int']);
const enumWindows = native(user, 'EnumWindows', 'bool', ['pointer', 'pointer']);
const post = native(user, 'PostMessageW', 'bool', ['pointer', 'uint', 'pointer', 'pointer']);
const pidBuffer = Memory.alloc(4);
const classBuffer = Memory.alloc(512);
let wakeWindow = nullPointer;
let guiThread = 0;
const enumCallback = new NativeCallback(window => {
  const thread = getPid(window, pidBuffer);
  if (pidBuffer.readU32() !== Process.id || !getParent(window).isNull()) return 1;
  getClass(window, classBuffer, 256);
  const name = classBuffer.readUtf16String();
  if (name.startsWith('Qt6') && name.endsWith('QWindowIcon')) {
    wakeWindow = window;
    guiThread = thread;
    return 0;
  }
  return 1;
}, 'bool', ['pointer', 'pointer']);
const originals = new Map();
let pending = false;
let restoring = false;
let pendingRequest = null;
let completeRequest = null;
let hook = null;
let deadline;
let lastSignature = '';
let lastStatus = { state: 'waiting-for-main-window', qtVersion };
function requestGui() {
  if (pendingRequest) return pendingRequest;
  wakeWindow = nullPointer;
  enumWindows(enumCallback, nullPointer);
  if (wakeWindow.isNull()) return Promise.resolve(lastStatus);
  pending = true;
  pendingRequest = new Promise(resolve => { completeRequest = resolve; });
  const result = pendingRequest;
  hook = Interceptor.attach(user.getExportByName('DispatchMessageW'), { onEnter: onGuiMessage });
  deadline = setTimeout(finishRequest, 1000);
  post(wakeWindow, 0, nullPointer, nullPointer);
  return result;
}
function finishRequest() {
  pending = false;
  if (hook) { hook.detach(); hook = null; }
  clearTimeout(deadline);
  const resolve = completeRequest;
  completeRequest = pendingRequest = null;
  if (resolve) resolve(lastStatus);
}
function onGuiMessage() {
    if (!pending || this.threadId !== guiThread) return;
    pending = false;
    // Instrument only this requested GUI operation, never every idle message.
    if (hook) { hook.detach(); hook = null; }
    try {
      const layouts = findLayouts();
      const current = new Set(layouts.flatMap(layout => [layout.loader.toString(), layout.separator.toString()]));
      for (const key of originals.keys()) if (!current.has(key)) originals.delete(key);
      for (const layout of layouts) {
        for (const item of [layout.loader, layout.separator]) {
          const key = item.toString();
          if (restoring) {
            if (originals.has(key)) setVisible(item, originals.get(key));
          } else {
            if (!originals.has(key) && originals.size < 64) originals.set(key, visible(item));
            if (visible(item)) setVisible(item, 0);
          }
        }
      }
      lastStatus = {
        state: restoring ? 'restored' : layouts.length ? 'applied' : 'waiting-for-ad-layout',
        qtVersion,
        pid: Process.id,
        windows: layouts.map(layout => ({
          handle: winId(layout.window).toString(),
          fullHeight: height(layout.root),
          contentHeight: height(layout.stack),
          advertisementVisible: visible(layout.loader),
          separatorVisible: visible(layout.separator),
        })),
      };
      const signature = JSON.stringify(lastStatus);
      if (signature !== lastSignature) {
        lastSignature = signature;
        send({ kind: 'layout-status', ...lastStatus });
      }
    } catch (error) {
      lastStatus = { state: 'layout-error', error: String(error), qtVersion };
      send({ kind: 'layout-error', error: String(error) });
    }
    finishRequest();
}
rpc.exports = {
  status() { return lastStatus; },
  ensure() { return restoring ? lastStatus : requestGui(); },
  restore() {
    restoring = true;
    return requestGui();
  },
};
send({ kind: 'layout-agent-ready', qtVersion, pid: Process.id });
