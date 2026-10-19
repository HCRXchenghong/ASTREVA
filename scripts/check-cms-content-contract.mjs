import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';
import { resolveFrontend } from './frontend-target.mjs';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
// 校验目标跟随 ACTIVE_FRONTEND / FRONTEND_DIR，新旧前端并行时各查各的，互不干扰。
const TARGET = resolveFrontend();
const TYPES_FILE = path.join(TARGET.src, 'lib', 'types.ts');
const SEED_FILE = path.join(ROOT, 'admin-server', 'seed', 'site-content.json');
const CONTENT_SOURCE_DIRS = [
  path.join(TARGET.src, 'pages'),
  path.join(TARGET.src, 'components'),
  path.join(TARGET.src, 'layouts')
];

function propertyType(checker, symbol) {
  const declaration = symbol.valueDeclaration || symbol.declarations?.[0];
  return declaration ? checker.getTypeOfSymbolAtLocation(symbol, declaration) : null;
}

function primitive(type) {
  return Boolean(type.flags & (
    ts.TypeFlags.StringLike |
    ts.TypeFlags.NumberLike |
    ts.TypeFlags.BooleanLike |
    ts.TypeFlags.BigIntLike |
    ts.TypeFlags.ESSymbolLike |
    ts.TypeFlags.Any |
    ts.TypeFlags.Unknown |
    ts.TypeFlags.Never
  ));
}

function matchesPrimitive(type, value) {
  if (type.flags & ts.TypeFlags.StringLike) return typeof value === 'string';
  if (type.flags & (ts.TypeFlags.NumberLike | ts.TypeFlags.BigIntLike)) return typeof value === 'number' && Number.isFinite(value);
  if (type.flags & ts.TypeFlags.BooleanLike) return typeof value === 'boolean';
  return true;
}

function unionMemberForValue(checker, type, value) {
  const candidates = type.types.filter((item) => !(item.flags & (ts.TypeFlags.Undefined | ts.TypeFlags.Null)));
  const primitiveCandidate = candidates.find((candidate) => primitive(candidate) && matchesPrimitive(candidate, value));
  if (primitiveCandidate) return primitiveCandidate;
  if (candidates.length === 1 || !value || typeof value !== 'object' || Array.isArray(value)) return candidates.length === 1 ? candidates[0] : null;
  return candidates.find((candidate) => candidate.getProperties().some((symbol) => {
    const candidateType = propertyType(checker, symbol);
    const key = symbol.getName();
    return candidateType?.isStringLiteral?.() && value[key] === candidateType.value;
  })) || null;
}

function requiredProperties(checker, type, value, fieldPath, missing, invalid, visiting = new Set()) {
  if (type.isUnion()) {
    const candidate = unionMemberForValue(checker, type, value);
    if (candidate) return requiredProperties(checker, candidate, value, fieldPath, missing, invalid, visiting);
    invalid.push(`${fieldPath || '根节点'}（不符合联合内容类型）`);
    return;
  }
  if (primitive(type)) {
    if (!matchesPrimitive(type, value)) invalid.push(`${fieldPath}（类型不正确）`);
    return;
  }
  if (value == null) {
    invalid.push(`${fieldPath}（不能为空）`);
    return;
  }
  if (checker.isArrayType(type)) {
    if (!Array.isArray(value)) {
      invalid.push(fieldPath + '（应为数组）');
      return;
    }
    const itemType = checker.getTypeArguments(type)[0];
    value.forEach((item, index) => requiredProperties(checker, itemType, item, fieldPath + '.' + index, missing, invalid, visiting));
    return;
  }

  if (typeof value !== 'object' || Array.isArray(value)) {
    invalid.push(`${fieldPath}（应为对象）`);
    return;
  }

  const typeKey = checker.typeToString(type);
  if (visiting.has(typeKey)) return;
  visiting.add(typeKey);
  for (const symbol of type.getProperties()) {
    const optional = Boolean(symbol.flags & ts.SymbolFlags.Optional);
    const key = symbol.getName();
    const pathWithKey = fieldPath ? fieldPath + '.' + key : key;
    if (!Object.prototype.hasOwnProperty.call(value, key)) {
      if (!optional) missing.push(pathWithKey);
      continue;
    }
    const nested = propertyType(checker, symbol);
    if (nested) requiredProperties(checker, nested, value[key], pathWithKey, missing, invalid, visiting);
  }
  visiting.delete(typeKey);
}

async function sourceFiles(directory) {
  const entries = await fs.readdir(directory, { withFileTypes: true });
  const nested = await Promise.all(entries.map(async (entry) => {
    const file = path.join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(file);
    return /\.(astro|tsx|ts)$/.test(entry.name) ? [file] : [];
  }));
  return nested.flat();
}

async function hardcodedFrontendContent() {
  const files = (await Promise.all(CONTENT_SOURCE_DIRS.map(sourceFiles))).flat();
  const findings = [];
  for (const file of files) {
    const text = await fs.readFile(file, 'utf8');
    const lines = text.split(/\r?\n/);
    lines.forEach((line, index) => {
      if (/[\u4e00-\u9fff]/.test(line)) findings.push(`${path.relative(ROOT, file)}:${index + 1}（发现页面硬编码文案）`);
      if (/https?:\/\//.test(line) &&
        !file.endsWith(path.join('components', 'VideoModal.tsx')) &&
        !file.endsWith(path.join('pages', 'robots.txt.ts'))) {
        findings.push(`${path.relative(ROOT, file)}:${index + 1}（发现页面硬编码媒体地址）`);
      }
    });
  }
  return findings;
}

export async function checkCmsContentContract() {
  const [rawSeed] = await Promise.all([fs.readFile(SEED_FILE, 'utf8'), fs.access(TYPES_FILE)]);
  const seed = JSON.parse(rawSeed);
  const program = ts.createProgram([TYPES_FILE], { strict: true, noEmit: true });
  const checker = program.getTypeChecker();
  const source = program.getSourceFile(TYPES_FILE);
  const declaration = source?.statements.find((statement) => ts.isTypeAliasDeclaration(statement) && statement.name.text === 'SiteContent');
  if (!declaration) throw new Error(`未找到 ${path.relative(ROOT, TYPES_FILE)} 中的 SiteContent 类型。`);
  const missing = [];
  const invalid = [];
  requiredProperties(checker, checker.getTypeFromTypeNode(declaration.type), seed, '', missing, invalid);
  const hardcoded = await hardcodedFrontendContent();
  return { ok: missing.length === 0 && invalid.length === 0 && hardcoded.length === 0, missing, invalid, hardcoded };
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const result = await checkCmsContentContract();
  if (!result.ok) {
    console.error('CMS 内容契约校验失败：');
    for (const field of result.missing) console.error('- ' + field);
    for (const field of result.invalid) console.error('- ' + field);
    for (const field of result.hardcoded) console.error('- ' + field);
    process.exitCode = 1;
  } else {
    console.log('CMS 内容契约校验通过：前端 SiteContent 的所有必填字段均可由后台配置。');
  }
}
