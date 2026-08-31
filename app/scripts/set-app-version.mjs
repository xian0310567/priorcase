// tauri.conf.json 의 version 을 릴리스 태그에 맞춘다.
//
// **판이 두 곳에서 따로 정해지면 어긋난다.** CLI 는 goreleaser 가 ldflags 로 박고
// 앱은 이 파일에 손으로 적혀 있다 — 안 맞추면 설치 화면이 옛 숫자를 보여 주고
// 사람은 그걸 받은 판이라고 읽는다. npm-pack.sh 가 바이너리에 같은 검사를 건다.
import { readFileSync, writeFileSync } from "node:fs";

const version = process.argv[2];
if (!version || !/^\d+\.\d+\.\d+/.test(version)) {
  console.error(`판이 이상하다: ${JSON.stringify(version)} (예: 0.5.0)`);
  process.exit(1);
}

const path = new URL("../src-tauri/tauri.conf.json", import.meta.url);
const conf = JSON.parse(readFileSync(path, "utf8"));
conf.version = version;
writeFileSync(path, JSON.stringify(conf, null, 2) + "\n");
console.log(`앱 판: ${version}`);
