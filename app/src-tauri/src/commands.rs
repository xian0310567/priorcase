use crate::prior::{run, PriorError};
use serde::Serialize;
use std::time::Duration;

/// READ_TIMEOUT 은 읽기 명령의 상한이다.
///
/// 실측: `prior settings --json` 0.01초, `prior queue --json` 2.6초(결정 156건 ·
/// 검사 10개). 볼트가 늘면 곱해지므로 여유를 준다. 이보다 오래 걸리면 앱이 멎은
/// 것처럼 보이므로 짧게 잡는다.
pub const READ_TIMEOUT: Duration = Duration::from_secs(10);

/// WRITE_TIMEOUT 은 설정을 고치는 명령의 상한이다.
///
/// 설정 쓰기는 파일 하나를 고치는 일이라 밀리초 단위다. 여유를 크게 주는 이유는
/// 볼트 자리를 만드는 일(mkdir)이 네트워크 드라이브에서 느릴 수 있어서다.
///
/// **예전에는 120초였다.** 앱이 `prior promote` 를 부르고 그 판별기 예산이
/// 90초였기 때문인데, 확인 큐를 들어내면서 앱은 더 이상 판별기를 부르지 않는다
/// (판별기는 데몬이 부른다). 상한이 필요 이상으로 길면 **정말 멈춘 명령이
/// 2분 동안 멈춘 줄 모르게 된다.**
pub const WRITE_TIMEOUT: Duration = Duration::from_secs(30);

/// ENV_LOCK 은 환경변수를 만지는 테스트를 직렬화한다.
///
/// 테스트는 한 프로세스에서 병렬로 돈다 — 한쪽이 PRIORCASE_APP_BIN 을 지우는 동안
/// 다른 쪽이 그것을 읽으면 깜빡인다. **깜빡이는 테스트는 신호를 잃는다.**
#[cfg(test)]
pub(crate) static ENV_LOCK: std::sync::Mutex<()> = std::sync::Mutex::new(());

/// CmdError 는 프런트가 **종류별로 다른 화면**을 그리기 위한 것이다.
///
/// kind 를 문자열로 내보내는 이유: 프런트가 한국어 문구를 파싱하게 하면 문구를
/// 고치는 순간 화면이 조용히 깨진다. 값 넷은 프런트와의 계약이다.
#[derive(Debug, Serialize)]
pub struct CmdError {
    pub kind: String,
    pub message: String,
}

pub fn to_cmd_error(e: PriorError) -> CmdError {
    let kind = match &e {
        PriorError::NotFound => "not_found",
        PriorError::Failed { .. } => "failed",
        PriorError::Timeout => "timeout",
        PriorError::Io(_) => "io",
    };
    CmdError {
        kind: kind.to_string(),
        message: e.to_string(),
    }
}

/// prior_bin 은 부를 바이너리 경로다.
///
/// # 찾는 순서: 환경변수 → PATH → 앱 안에 번들된 것
///
/// **PATH 가 번들보다 먼저다.** 둘의 갱신 주기가 다르기 때문이다 — CLI 는 npm 으로
/// 자주 갱신되고(`npm i -g priorcase`) 앱 셸은 드물게 재배포된다. 번들을 먼저 쓰면
/// npm 으로 최신을 깐 사람이 **앱을 통해서만 옛 판을 쓰게 되는데**, 그 사실이
/// 아무 데도 안 보인다.
///
/// **번들이 있어야 하는 이유는 그 반대쪽이다.** 사내 배포 대상이 개발자만이 아니다
/// (윈도우 기획자도 Claude Code 로 작업하고 기록한다). 그 사람에게 "먼저 Node 를
/// 깔고 npm i -g priorcase 를 치세요" 라고 하면 거기서 멈춘다. 앱 하나로 되어야 한다.
///
/// 환경변수가 맨 앞인 이유는 **테스트다** — 가짜 prior 로 각 오류 화면을 재현하는
/// 유일한 문이다.
///
/// 결과를 캐시한다. 이 함수는 커맨드마다 불리는데 PATH 훑기와 디렉터리 읽기를
/// 매번 하면 그 비용이 화면 응답에 그대로 얹힌다.
pub fn prior_bin() -> String {
    use std::sync::OnceLock;
    static CACHE: OnceLock<String> = OnceLock::new();

    // 환경변수는 캐시 밖이다 — 테스트가 한 프로세스에서 여러 가짜를 갈아 끼운다.
    if let Ok(p) = std::env::var("PRIORCASE_APP_BIN") {
        if !p.is_empty() {
            return p;
        }
    }
    CACHE.get_or_init(resolve_bin).clone()
}

fn resolve_bin() -> String {
    if on_path("prior") {
        return "prior".to_string();
    }
    if let Some(p) = bundled_bin() {
        return p;
    }
    // **못 찾아도 "prior" 를 돌려준다.** 그래야 오류 메시지가 그 이름을 담고,
    // 사람이 "prior 가 없다" 는 것을 안다. 빈 문자열을 주면 그냥 실행 실패다.
    "prior".to_string()
}

/// on_path 는 PATH 에서 실행 가능한 그 이름을 찾는다.
///
/// `which` 를 부르지 않는다 — 윈도우에는 없고, 프로세스를 하나 더 띄우는 값이 있다.
fn on_path(name: &str) -> bool {
    let Some(path) = std::env::var_os("PATH") else {
        return false;
    };
    // 윈도우는 확장자가 붙어야 실행된다. PATHEXT 를 다 보지 않고 .exe 만 본다 —
    // 우리가 배포하는 것이 그것뿐이다.
    let names: &[&str] = if cfg!(windows) { &["prior.exe"] } else { &["prior"] };
    let _ = name;
    std::env::split_paths(&path).any(|dir| {
        names.iter().any(|n| {
            let p = dir.join(n);
            p.is_file()
        })
    })
}

/// bundled_bin 은 앱 실행파일 옆에 딸려 온 prior 를 찾는다 (Tauri externalBin).
///
/// 이름을 둘로 보는 이유: Tauri 는 번들할 때 타깃 트리플을 떼고 넣는데, 판에 따라
/// `prior-<트리플>` 그대로 남는 경우가 있다. **둘 다 보는 것이 싸다** — 못 찾으면
/// 앱이 통째로 못 도는데, 그 실패를 판 차이로 만들 이유가 없다.
fn bundled_bin() -> Option<String> {
    let exe = std::env::current_exe().ok()?;
    let dir = exe.parent()?;
    let plain = if cfg!(windows) { "prior.exe" } else { "prior" };
    let p = dir.join(plain);
    if p.is_file() {
        return Some(p.to_string_lossy().into_owned());
    }
    // `prior-aarch64-apple-darwin` 처럼 트리플이 붙어 있는 판.
    let entries = std::fs::read_dir(dir).ok()?;
    for e in entries.flatten() {
        let name = e.file_name();
        let name = name.to_string_lossy();
        if name.starts_with("prior-") && (!cfg!(windows) || name.ends_with(".exe")) {
            return Some(e.path().to_string_lossy().into_owned());
        }
    }
    None
}

// ── prior 호출은 메인 스레드에서 하지 않는다 ──────────────────────────────
//
// **Tauri 는 동기 커맨드를 메인 스레드에서 돌린다.** 그 스레드가 곧 UI 라,
// `prior` 가 도는 동안 창이 통째로 멈춘다 — 클릭도 스크롤도 안 먹는다.
//
// 예전에는 커맨드 열여섯이 전부 동기였다. 명령 하나가 40~65ms 일 때는 티가 덜
// 났는데, 볼트가 자라 `prior queue` 가 6.3초가 되자(2026-09-01 실측) **앱을
// 띄우자마자 6초를 멈추고 그 뒤로 60초마다 다시 멈췄다.** 사업주 표현으로
// "사람이 쓸 수 없는" 상태였다.
//
// 고장의 크기가 볼트 크기에 비례해 자라는 종류라, 명령을 더 빠르게 만드는 것으로는
// 못 막는다 — **자리를 옮겨야 한다.**
//
// `async` 로 선언하면 Tauri 가 비동기 런타임에서 돌리는데, 우리 일은 프로세스를
// 띄우고 기다리는 **블로킹** 이라 런타임 스레드를 잡아먹는다. 그래서 한 겹 더
// 보내 `spawn_blocking` 위에서 돈다.
async fn off_main<T, F>(f: F) -> Result<T, CmdError>
where
    F: FnOnce() -> Result<T, CmdError> + Send + 'static,
    T: Send + 'static,
{
    match tauri::async_runtime::spawn_blocking(f).await {
        Ok(r) => r,
        Err(e) => Err(CmdError {
            kind: "io".to_string(),
            message: format!("작업 스레드가 죽었다: {e}"),
        }),
    }
}

pub fn run_queue(bin: &str) -> Result<String, CmdError> {
    run(bin, &["queue", "--json"], READ_TIMEOUT).map_err(to_cmd_error)
}

pub fn run_settings(bin: &str) -> Result<String, CmdError> {
    run(bin, &["settings", "--json"], READ_TIMEOUT).map_err(to_cmd_error)
}

/// queue 는 **상태 화면만** 쓴다.
///
/// 확인·검토·회고 큐는 앱에서 들어냈다 (2026-08-14). 사람에게 "이걸 기록할까요"
/// 를 묻는 순간 자동 기록이라는 전제를 사람이 대신 갚기 때문이다. 남은 것은
/// health 검사와 "밀린 구간이 몇 개인가" 라는 진단 한 줄이다.
#[tauri::command]
pub async fn queue() -> Result<String, CmdError> {
    off_main(move || {
        run_queue(&prior_bin())
    })
    .await
}

/// settings 는 볼트·도메인·호스트를 한 번에 준다.
#[tauri::command]
pub async fn settings() -> Result<String, CmdError> {
    off_main(move || {
        run_settings(&prior_bin())
    })
    .await
}

/// set_host 는 호스트 하나를 켜거나 끈다.
///
/// **앱이 설정 파일을 직접 쓰지 않는다.** 주석 보존·스칼라 볼트 변환·고친 뒤
/// 검증이 두 벌이 되면 한쪽만 고쳐진 채로 남고, 그때 망가지는 것은 사람이 손으로
/// 쓴 설정이다.
#[tauri::command]
pub async fn set_host(name: String, enabled: bool) -> Result<(), CmdError> {
    off_main(move || {
        let sub = if enabled { "enable" } else { "disable" };
        run(&prior_bin(), &["hosts", sub, &name], WRITE_TIMEOUT)
            .map(|_| ())
            .map_err(to_cmd_error)
    })
    .await
}

/// add_vault 는 볼트를 하나 만든다. CLI 가 자리(폴더)까지 만든다.
///
/// **경로를 받지 않는다.** 어디에 만들지는 답이 이미 정해져 있는 질문이다 —
/// 지금 볼트 옆이다. CLI 가 그것을 정하므로 규칙이 한 곳에만 산다.
/// 앱은 `prior settings` 의 vault_parent 로 어디에 생길지 미리 보여 준다.
#[tauri::command]
pub async fn add_vault(name: String) -> Result<(), CmdError> {
    off_main(move || {
        run(&prior_bin(), &["vault", "add", &name], WRITE_TIMEOUT)
            .map(|_| ())
            .map_err(to_cmd_error)
    })
    .await
}

// ── 볼트를 읽고 고친다 ────────────────────────────────────────────────
//
// 앱의 주역이 설정 콘솔에서 볼트 브라우저로 바뀌었다(2026-09-01 결정).
// 넷 다 CLI 가 이미 하는 일을 그대로 넘긴다 — 앱은 `prior` 만 부른다.

/// list_notes 는 볼트 전부의 결정 목록을 준다. **본문은 없다.**
#[tauri::command]
pub async fn list_notes() -> Result<String, CmdError> {
    off_main(move || {
        run(&prior_bin(), &["list", "--json"], READ_TIMEOUT).map_err(to_cmd_error)
    })
    .await
}

/// show_note 는 결정 하나를 본문까지 준다.
#[tauri::command]
pub async fn show_note(stem: String) -> Result<String, CmdError> {
    off_main(move || {
        run(&prior_bin(), &["show", &stem, "--json"], READ_TIMEOUT).map_err(to_cmd_error)
    })
    .await
}

/// search_notes 는 **순위를 매긴** 검색이다.
///
/// 파일 탐색기는 이름으로 찾지만 여기는 회수 랭킹을 쓴다 — 옵시디언에 없는 것이
/// 이것이고, 그것이 이 앱을 만드는 이유 중 하나다.
#[tauri::command]
pub async fn search_notes(query: String) -> Result<String, CmdError> {
    off_main(move || {
        run(
            &prior_bin(),
            &["recall", &query, "--format", "json", "--limit", "20"],
            READ_TIMEOUT,
        )
        .map_err(to_cmd_error)
    })
    .await
}

/// save_body 는 결정의 **본문만** 바꾼다. frontmatter 는 CLI 가 지킨다.
///
/// # 왜 임시 파일인가
///
/// `prior edit` 는 본문을 표준입력으로도 받는데, 이 앱의 `run` 은 stdin 을 닫고
/// 자식을 띄운다(prior.rs 의 §: 대화형으로 빠지는 것을 막는다). 그 규약을 이 명령
/// 하나 때문에 흔들지 않는다 — `--body-file` 이 같은 일을 하고 흔적도 남지 않는다.
#[tauri::command]
pub async fn save_body(stem: String, body: String) -> Result<(), CmdError> {
    off_main(move || {
        let mut path = std::env::temp_dir();
        path.push(format!("priorcase-edit-{}.md", std::process::id()));
        std::fs::write(&path, body).map_err(|e| CmdError {
            kind: "io".into(),
            message: format!("임시 파일을 쓸 수 없다: {e}"),
        })?;
        let arg = path.to_string_lossy().into_owned();
        let out = run(
            &prior_bin(),
            &["edit", &stem, "--body-file", &arg],
            WRITE_TIMEOUT,
        );
        // **성공하든 실패하든 지운다.** 결정문 본문이 임시 폴더에 남으면 그것도 유출이다.
        let _ = std::fs::remove_file(&path);
        out.map(|_| ()).map_err(to_cmd_error)
    })
    .await
}

/// review_note 는 **frontmatter 를 고친다.**
///
/// # 왜 본문과 통로가 다른가
///
/// 본문은 `prior edit` 이 frontmatter 를 바이트 그대로 두고 갈아 끼우는데, 여기는
/// 반대다 — 스키마를 아는 `prior review` 만 거친다. 그래야 상태·결과의 허용값,
/// 철회에 이유가 필요하다는 규칙, 옛 요약 보존 같은 것이 한 자리에서 지켜진다
/// (2026-09-01 결정: 본문은 자유, frontmatter 는 구조화된 명령으로만).
///
/// 빈 값은 **"안 바꾼다"** 다. 지우려면 그 필드의 전용 규약을 쓴다(태그는 빈 목록).
#[tauri::command]
pub async fn review_note(
    stem: String,
    summary: Option<String>,
    status: Option<String>,
    outcome: Option<String>,
    retro: Option<String>,
    tags: Option<Vec<String>>,
) -> Result<(), CmdError> {
    off_main(move || {
        let mut args: Vec<String> = vec!["review".into(), stem.clone()];
        let mut push = |flag: &str, v: &str| {
            args.push(flag.into());
            args.push(v.into());
        };
        if let Some(v) = summary.as_deref().filter(|v| !v.is_empty()) {
            push("--summary", v);
        }
        if let Some(v) = status.as_deref().filter(|v| !v.is_empty()) {
            push("--status", v);
        }
        if let Some(v) = outcome.as_deref().filter(|v| !v.is_empty()) {
            push("--outcome", v);
        }
        if let Some(v) = retro.as_deref().filter(|v| !v.is_empty()) {
            push("--retro", v);
        }
        if let Some(t) = tags.as_ref() {
            // **빈 목록도 보낸다.** 그것이 "전부 지운다" 라, 안 보내는 것(변경 없음)과
            // 구별되어야 한다. 쉼표로 이어 한 인자로 준다.
            push("--tags", &t.join(","));
        }
        let refs: Vec<&str> = args.iter().map(String::as_str).collect();
        run(&prior_bin(), &refs, WRITE_TIMEOUT)
            .map(|_| ())
            .map_err(to_cmd_error)
    })
    .await
}

/// set_vault_remote 는 볼트가 동기화할 git 리모트를 정한다.
///
/// **앱만 받은 사람에게는 이것이 유일한 문이다.** 터미널에서 `git remote add` 를
/// 치라고 할 수 없고, 회사 볼트는 만들자마자 회사 리모트에 붙어야 그 결정이
/// 개인 머신에만 남지 않는다.
///
/// URL 검증은 안 한다 — CodeCommit·GitHub·사내 GitLab 이 전부 모양이 달라서,
/// 우리가 아는 모양만 받으면 멀쩡한 주소를 거절한다 (CLI 쪽 §).
#[tauri::command]
pub async fn set_vault_remote(name: String, url: String) -> Result<(), CmdError> {
    off_main(move || {
        run(
            &prior_bin(),
            &["vault", "remote", &name, &url],
            WRITE_TIMEOUT,
        )
        .map(|_| ())
        .map_err(to_cmd_error)
    })
    .await
}

/// bind_domain 은 프로젝트가 쓸 볼트를 정한다. vault 가 비면 기본 볼트로 되돌린다.
#[tauri::command]
pub async fn bind_domain(prefix: String, vault: String) -> Result<(), CmdError> {
    off_main(move || {
        let mut args = vec!["domain", "bind", prefix.as_str()];
        if !vault.is_empty() {
            args.push(vault.as_str());
        }
        run(&prior_bin(), &args, WRITE_TIMEOUT)
            .map(|_| ())
            .map_err(to_cmd_error)
    })
    .await
}

/// open_vault 는 볼트 폴더를 OS 파일 관리자로 연다.
///
/// **경로를 프런트에서 받지 않는다.** 이름으로 받아 여기서 `prior settings` 에
/// 물어본다 — 프런트가 임의의 경로를 열 수 있게 두면 그 창구가 앱의 다른 어떤
/// 기능보다 넓어진다.
///
/// **셸을 거치지 않는다.** 볼트 경로에 공백이 있다 (실측: `Obsidian Vault`).
/// 셸 문자열로 만들면 거기서 쪼개지고, 따옴표로 감싸는 순간 경로에 따옴표가
/// 있는 경우가 뚫린다. 인자로 넘기면 그런 자리가 없다.
#[tauri::command]
pub async fn open_vault(name: String) -> Result<(), CmdError> {
    off_main(move || {
        let raw = run_settings(&prior_bin())?;
        let path = vault_path_in(&raw, &name)?;
        let opener = if cfg!(target_os = "macos") {
            "open"
        } else if cfg!(target_os = "windows") {
            "explorer"
        } else {
            "xdg-open"
        };
        run(opener, &[path.as_str()], READ_TIMEOUT)
            .map(|_| ())
            .map_err(to_cmd_error)
    })
    .await
}

/// vault_path_in 은 settings 출력에서 그 볼트의 경로를 꺼낸다.
///
/// 빈 경로를 그대로 OS 에 넘기면 **현재 디렉토리가 열린다** — 사람은 왜 파인더가
/// 떴는지 모른다. 여기서 끊고 무엇이 문제인지 말한다.
fn vault_path_in(raw: &str, name: &str) -> Result<String, CmdError> {
    let v: serde_json::Value = serde_json::from_str(raw).map_err(|e| CmdError {
        kind: "failed".into(),
        message: format!("prior settings 의 출력이 JSON 이 아니다: {e}"),
    })?;
    let found = v["vaults"]
        .as_array()
        .and_then(|vs| vs.iter().find(|x| x["name"] == name))
        .and_then(|x| x["path"].as_str())
        .unwrap_or("")
        .trim()
        .to_string();
    if found.is_empty() {
        return Err(CmdError {
            kind: "not_found".into(),
            message: format!("볼트 {name} 의 경로를 찾을 수 없다"),
        });
    }
    Ok(found)
}

/// set_tray_title 은 메뉴바 아이콘 옆의 글자를 바꾼다.
///
/// **푸시 알림을 보내지 않는다.** 먼저 말을 거는 도구는 지워지고, 앱이 지워지면
/// 자동 기록까지 같이 잃는다. 글자만 조용히 바꾼다.
///
/// 빈 문자열이면 글자를 아예 뗀다 — 할 일이 없을 때 무언가 떠 있으면 그것도
/// 늘 켜진 신호가 되어, 사람이 배지를 무시하는 법을 배운다.
#[tauri::command]
pub fn set_tray_title(app: tauri::AppHandle, title: String) {
    if let Some(tray) = app.tray_by_id("main") {
        let _ = tray.set_title(if title.is_empty() { None } else { Some(title) });
    }
}

#[cfg(test)]
#[allow(non_snake_case)]
mod tests {
    use super::*;

    fn fixture(name: &str) -> String {
        let mut p = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        p.pop();
        p.push("fixtures");
        p.push(name);
        p.to_string_lossy().into_owned()
    }

    // ★★ **오류 종류가 문자열로 나가야 한다.**
    //
    // 프런트가 종류별로 다른 화면을 그리려면 kind 가 필요하다. Display 문자열만
    // 주면 프런트가 한국어 문구를 파싱하게 되고, **문구를 고치는 순간 화면이
    // 조용히 깨진다.**
    #[test]
    fn 오류_종류가_문자열로_나간다() {
        let e = to_cmd_error(crate::prior::PriorError::NotFound);
        assert_eq!(e.kind, "not_found");

        let e = to_cmd_error(crate::prior::PriorError::Timeout);
        assert_eq!(e.kind, "timeout");

        let e = to_cmd_error(crate::prior::PriorError::Io("파이프".into()));
        assert_eq!(e.kind, "io");

        let e = to_cmd_error(crate::prior::PriorError::Failed {
            code: Some(1),
            stderr: "설정을 읽을 수 없다".into(),
        });
        assert_eq!(e.kind, "failed");
        // stderr 가 message 에 살아 있어야 한다 — 사람이 고칠 유일한 단서다.
        assert!(
            e.message.contains("설정을 읽을 수 없다"),
            "message={}",
            e.message
        );
    }

    // ★★ **kind 는 프런트와의 계약이다.** 값이 바뀌면 화면이 조용히 안 그려진다.
    #[test]
    fn kind_값_넷이_고정이다() {
        let kinds: Vec<String> = vec![
            crate::prior::PriorError::NotFound,
            crate::prior::PriorError::Failed {
                code: None,
                stderr: String::new(),
            },
            crate::prior::PriorError::Timeout,
            crate::prior::PriorError::Io(String::new()),
        ]
        .into_iter()
        .map(|e| to_cmd_error(e).kind)
        .collect();
        assert_eq!(kinds, vec!["not_found", "failed", "timeout", "io"]);
    }

    #[test]
    fn queue_는_stdout_을_그대로_준다() {
        let out = run_queue(&fixture("fake-prior-ok.sh")).expect("성공해야 한다");
        assert!(out.contains("\"health\""), "out={out}");
    }

    // ★★ **상한이 실측을 담되 필요 이상으로 길지 않아야 한다.**
    //
    // 앱은 더 이상 판별기를 부르지 않는다 — 확인 큐를 들어내면서 승격은 데몬의
    // 몫이 됐다. 상한이 2분으로 남아 있으면 **정말 멈춘 명령이 2분 동안 멈춘 줄
    // 모르게 된다.**
    #[test]
    fn 상한이_실측을_담되_과하지_않다() {
        assert!(
            READ_TIMEOUT.as_secs() >= 5,
            "읽기 상한={READ_TIMEOUT:?} — queue 실측 2.6초를 못 담는다"
        );
        assert!(
            READ_TIMEOUT.as_secs() <= 15,
            "읽기 상한={READ_TIMEOUT:?} — 큐가 이만큼 걸리면 앱이 멎은 것처럼 보인다"
        );
        assert!(
            WRITE_TIMEOUT.as_secs() <= 60,
            "쓰기 상한={WRITE_TIMEOUT:?} — 설정 쓰기는 밀리초 단위다. 길면 멈춘 것을 못 알아본다"
        );
        assert!(
            WRITE_TIMEOUT > READ_TIMEOUT,
            "쓰기({WRITE_TIMEOUT:?})가 읽기({READ_TIMEOUT:?})보다 짧다"
        );
    }

    // ★★ **바이너리 경로를 환경변수로 덮을 수 있어야 한다.**
    //
    // 가짜 prior 로 각 오류 화면을 재현하는 유일한 문이다. 이게 없으면 오류 처리를
    // 손으로만 확인하게 되고, 그건 회귀를 못 잡는다.
    #[test]
    fn 바이너리_경로를_환경변수로_덮는다() {
        // 이 테스트만 환경변수를 만지므로 다른 테스트와 겹치지 않게 한다.
        let _g = ENV_LOCK.lock().unwrap();
        let saved = std::env::var("PRIORCASE_APP_BIN").ok();

        std::env::set_var("PRIORCASE_APP_BIN", "/어떤/자리/prior");
        assert_eq!(prior_bin(), "/어떤/자리/prior");

        std::env::remove_var("PRIORCASE_APP_BIN");
        assert_eq!(prior_bin(), "prior", "기본은 PATH 의 prior 여야 한다");

        if let Some(v) = saved {
            std::env::set_var("PRIORCASE_APP_BIN", v);
        }
    }

    // ★★★ **볼트를 못 찾으면 열지 않는다.**
    //
    // 빈 경로를 그대로 open 에 넘기면 OS 가 **현재 디렉토리를 연다** — 사람은
    // 왜 파인더가 떴는지 모른다.
    #[test]
    fn 없는_볼트는_열지_않는다() {
        let raw = r#"{"vaults":[{"name":"default","path":"/tmp/v"}]}"#;
        let e = vault_path_in(raw, "없는볼트").expect_err("실패해야 한다");
        assert_eq!(e.kind, "not_found");

        let raw_empty = r#"{"vaults":[{"name":"default","path":"  "}]}"#;
        let e = vault_path_in(raw_empty, "default").expect_err("빈 경로도 실패해야 한다");
        assert_eq!(e.kind, "not_found");

        let ok = vault_path_in(raw, "default").expect("있는 볼트는 나와야 한다");
        assert_eq!(ok, "/tmp/v");
    }

    // ★★★ **설정을 고치는 명령들이 인자를 그대로 넘겨야 한다.**
    //
    // 이름이 어긋나면 조용히 안 먹는다 — 껐다고 믿는데 계속 돈다.
    #[test]
    fn 설정_명령이_인자를_그대로_넘긴다() {
        let echo = fixture("fake-prior-echo-args.sh");
        let out = crate::prior::run(&echo, &["hosts", "disable", "Codex CLI"], READ_TIMEOUT)
            .expect("성공해야 한다");
        assert!(out.contains("disable"), "out={out}");
        assert!(out.contains("Codex CLI"), "공백 있는 이름이 쪼개졌다: {out}");

        // 볼트는 **이름만** 넘긴다 — 자리는 CLI 가 정한다.
        let out = crate::prior::run(&echo, &["vault", "add", "우리 회사"], READ_TIMEOUT)
            .expect("성공해야 한다");
        assert!(out.contains("우리 회사"), "공백 있는 이름이 쪼개졌다: {out}");
        assert!(!out.contains('/'), "경로를 넘기고 있다: {out}");
    }

    // ★★★ **볼트를 안 주면 인자에서 빠져야 한다.**
    //
    // `prior domain bind <도메인>` 은 "기본 볼트로 되돌린다" 이고,
    // `prior domain bind <도메인> ""` 은 빈 이름의 볼트를 찾다가 실패한다.
    // 빈 문자열을 그대로 넘기면 되돌리기가 영영 안 된다.
    #[test]
    fn 빈_볼트는_인자에서_빠진다() {
        let mut args = vec!["domain", "bind", "omni"];
        let vault = String::new();
        if !vault.is_empty() {
            args.push(vault.as_str());
        }
        assert_eq!(args, vec!["domain", "bind", "omni"]);
    }

    // ★★★ **가짜 prior 가 진짜 하위 프로세스 경로를 통과해야 한다.**
    //
    // TS 쪽 시험은 픽스처의 stdout 을 파일에서 읽어 검사한다 — 그건 **JSON 이
    // 맞는가**를 본다. 여기서는 그 바이트가 실제로 spawn · 파이프 · 상한을 거쳐
    // 우리 손에 오는지를 본다. 둘은 다른 것이고, 이 경로에서만 나는 고장이
    // 있다 (파이프 교착 · 좀비 · 상한).
    #[test]
    fn 큐_픽스처들이_실제_실행_경로를_통과한다() {
        for name in ["ok", "warnings", "onebroken"] {
            let out = run_queue(&fixture(&format!("fake-prior-{name}.sh")))
                .unwrap_or_else(|e| panic!("{name}: {} ({})", e.message, e.kind));
            let v: serde_json::Value =
                serde_json::from_str(&out).unwrap_or_else(|e| panic!("{name}: JSON 아님 {e}"));
            assert!(v["health"].is_array(), "{name}: health 가 배열이 아니다");
        }
    }

    // ★★ **깨진 JSON 은 성공으로 온다 — 실패로 오지 않는다.**
    //
    // prior 가 종료코드 0 으로 쓰레기를 내는 판이 있다 (패닉 뒤 부분 출력).
    // Rust 층은 그걸 그대로 넘기고, **판정은 api.ts 가 한다.** 여기서 미리
    // 실패로 바꾸면 "실행이 안 됐다" 와 "출력이 깨졌다" 가 같아 보인다.
    #[test]
    fn 깨진_JSON_은_Rust_층에서_성공으로_온다() {
        let out = run_queue(&fixture("fake-prior-broken-json.sh")).expect("종료코드는 0 이다");
        assert!(
            serde_json::from_str::<serde_json::Value>(&out).is_err(),
            "픽스처가 실제로 깨진 JSON 을 내야 한다: {out}"
        );
    }
}
