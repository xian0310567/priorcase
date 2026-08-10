#!/usr/bin/env node
// casebook 런처.
//
// **이 파일은 아무 일도 하지 않는다** — 자기 플랫폼의 네이티브 바이너리를 찾아
// 그대로 넘긴다. esbuild 가 쓰는 방식이고, 이유가 셋이다.
//
//   1. 설치 시 다운로드가 없다. npm 이 optionalDependencies 로 자기 플랫폼 것만
//      받는다 — postinstall 스크립트가 네트워크를 타면 사내망·오프라인에서 죽는다.
//   2. Node 는 **배달부일 뿐이다.** 실행되는 것은 정적 Go 바이너리이고,
//      런타임 의존은 여전히 0 이다 (D1).
//   3. `npx -y casebook mcp` 가 그대로 된다 — MCP 생태계의 규범이다.
//
// exec 로 프로세스를 갈아탄다. 감싸면 신호(Ctrl-C)와 종료 코드가 한 겹 더 거쳐야
// 하는데, casebook 은 훅으로 불려서 **종료 코드가 규약**이다.

const { spawnSync } = require("child_process");
const { existsSync } = require("fs");
const path = require("path");

const PKG = { darwin: "darwin", linux: "linux" };
const ARCH = { arm64: "arm64", x64: "x64" };

function binaryPath() {
  const os = PKG[process.platform];
  const arch = ARCH[process.arch];
  if (!os || !arch) return null;
  // 스코프를 안 쓴다 — npm 조직 이름 casebook 이 막혀 있다.
  // 사용자가 치는 명령(npx -y casebook mcp)에는 스코프가 안 나오므로 차이가 없다.
  const pkg = `casebook-${os}-${arch}`;
  try {
    // require.resolve 가 node_modules 해석을 대신한다 — 경로를 직접 짜면
    // pnpm·yarn PnP 같은 배치에서 깨진다.
    const entry = require.resolve(`${pkg}/package.json`);
    const p = path.join(path.dirname(entry), "bin", "cb");
    return existsSync(p) ? p : null;
  } catch {
    return null;
  }
}

const bin = binaryPath();
if (!bin) {
  const target = `${process.platform}-${process.arch}`;
  process.stderr.write(
    `casebook: no binary for ${target}.\n` +
      `  Supported: darwin-arm64, darwin-x64, linux-arm64, linux-x64.\n` +
      `  If your platform is listed, the optional dependency did not install —\n` +
      `  try: npm install --include=optional casebook\n`
  );
  // 훅은 언제나 exit 0 이어야 하지만, 여기는 **설치가 안 된 상태**다.
  // 그건 조용히 넘어갈 일이 아니다 — cb doctor 가 볼 수 있도록 실패로 낸다.
  process.exit(1);
}

const r = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
if (r.error) {
  process.stderr.write(`casebook: ${r.error.message}\n`);
  process.exit(1);
}
// 신호로 죽었으면 그 신호를 그대로 흉내 낸다 — 상위 셸이 Ctrl-C 를 알아야 한다.
if (r.signal) {
  process.kill(process.pid, r.signal);
}
process.exit(r.status === null ? 1 : r.status);
