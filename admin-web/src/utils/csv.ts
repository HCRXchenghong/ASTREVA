/**
 * CSV 单元格转义：
 * - 引号翻倍，保证 CSV 语法正确；
 * - 对以 = + - @ 或制表符开头的值前置单引号，阻止 Excel/LibreOffice 公式注入；
 * - 去掉 CR/LF，避免同一单元格拆行。
 */
export function csvCell(value: unknown): string {
  let text = String(value ?? '').replace(/[\r\n]+/g, ' ');
  if (text && /^[=+\-@\t]/.test(text)) text = `'${text}`;
  return `"${text.replace(/"/g, '""')}"`;
}

export function csvRows(rows: unknown[][]): string {
  return rows.map((row) => row.map(csvCell).join(',')).join('\r\n');
}
