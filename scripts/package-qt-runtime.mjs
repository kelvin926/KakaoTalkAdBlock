import fs from 'node:fs/promises';
import path from 'node:path';

if (process.platform !== 'win32' || process.arch !== 'x64') {
  throw new Error('Package the Qt layout runtime with Windows x64 Node.js.');
}
const directory = path.resolve(import.meta.dirname, '..', 'runtime');
await fs.access(path.join(directory, 'node_modules', 'frida', 'build', 'frida_binding.node'));
const destination = path.join(directory, 'node.exe');
if (path.resolve(process.execPath).toLowerCase() !== destination.toLowerCase()) {
  await fs.copyFile(process.execPath, destination);
}
console.log('Portable Qt layout runtime prepared in runtime/. Omit runtime/state/ from distribution.');
