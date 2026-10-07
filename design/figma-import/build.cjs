const fs = require('node:fs');
const path = require('node:path');
const source = process.argv[2];
if (!source) throw new Error('Pass the original reference screenshot path.');
const bytes = fs.readFileSync(source);
if (bytes.readUInt32BE(16) !== 2408 || bytes.readUInt32BE(20) !== 885) {
  throw new Error('Reference screenshot must be 2408 × 885 for the recorded crops.');
}
const script = fs.readFileSync(path.join(__dirname, 'source.js'), 'utf8');
fs.writeFileSync(path.join(__dirname, 'code.js'),
  '// Built local Figma import. No network requests.\n' +
  'const REFERENCE_PNG = ' + JSON.stringify(bytes.toString('base64')) + ';\n' + script);
console.log('Built code.js with embedded user-provided reference image.');
