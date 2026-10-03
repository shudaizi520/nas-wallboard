const releasePattern = /^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;

export function normalizeReleaseVersion(value) {
  if (typeof value !== 'string' || value.length > 96 || !releasePattern.test(value) || /\s/.test(value)) return null;
  return `v${value.replace(/^v/, '').split('+')[0]}`;
}

export function clientVersionFromSearch(search = '') {
  return normalizeReleaseVersion(new URLSearchParams(search).get('client_version'));
}

function compareVersions(left, right) {
  const parts = (version) => {
    const tag = version.slice(1); const split = tag.indexOf('-');
    return split < 0 ? [tag, undefined] : [tag.slice(0, split), tag.slice(split + 1)];
  };
  const [leftMain, leftPre] = parts(left);
  const [rightMain, rightPre] = parts(right);
  const leftNumbers = leftMain.split('.').map(BigInt);
  const rightNumbers = rightMain.split('.').map(BigInt);
  for (let index = 0; index < 3; index += 1) {
    if (leftNumbers[index] !== rightNumbers[index]) return leftNumbers[index] > rightNumbers[index] ? 1 : -1;
  }
  if (leftPre === rightPre) return 0;
  if (leftPre === undefined) return 1;
  if (rightPre === undefined) return -1;
  const leftParts = leftPre.split('.'); const rightParts = rightPre.split('.');
  for (let index = 0; index < Math.max(leftParts.length, rightParts.length); index += 1) {
    const a = leftParts[index]; const b = rightParts[index];
    if (a === b) continue;
    if (a === undefined) return -1;
    if (b === undefined) return 1;
    const numericA = /^[0-9]+$/.test(a); const numericB = /^[0-9]+$/.test(b);
    if (numericA && numericB) {
      if (BigInt(a) === BigInt(b)) continue;
      return BigInt(a) > BigInt(b) ? 1 : -1;
    }
    if (numericA !== numericB) return numericA ? -1 : 1;
    return a > b ? 1 : -1;
  }
  return 0;
}

export function clientVersionStatus(packaged, installed) {
  const available = normalizeReleaseVersion(packaged);
  const current = normalizeReleaseVersion(installed);
  const value = `${current ? `当前客户端 ${current}` : '已安装版本未知'} · ${available ? `可下载 ${available}` : '下载版本未知'}`;
  if (!current) return {value, tone:'neutral', detail:'普通浏览器无法读取电脑上已安装的客户端版本；请从托盘“打开内容管理”进入，或查看“关于与版本”。'};
  if (!available) return {value, tone:'neutral', detail:'下载包没有有效版本信息，无法判断是否需要更新客户端。'};
  const order = compareVersions(current, available);
  if (order < 0) return {value, tone:'warn', detail:'客户端版本由本次打开管理页的桌面端提供；请退出后重新下载并替换程序，刷新不会更新客户端。'};
  if (order > 0) return {value, tone:'neutral', detail:'客户端版本由本次打开管理页的桌面端提供，较下载包更新；不要用旧包覆盖。'};
  return {value, tone:'neutral', detail:'客户端版本由本次打开管理页的桌面端提供，与下载包一致；网页内容变更只需刷新，无需重新安装。'};
}

// Carry only the display-only release identity, never a user-controlled redirect.
export function managementAddress(path, search = '') {
  const version = clientVersionFromSearch(search);
  return version ? `${path}?client_version=${encodeURIComponent(version)}` : path;
}

export function updateMessage(update = {}) {
  if (update.error) return update.error;
  if (update.disabled) return '未启用更新检查，无法确认最新版本';
  if (update.available) return `发现 NAS 服务端新版本 ${update.latest}；桌面客户端版本需单独核对`;
  if (!update.latest) return '尚无有效更新结果，无法确认最新版本';
  return 'NAS 服务端已是最新版本；桌面客户端请看下方版本信息';
}
