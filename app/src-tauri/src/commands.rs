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
/// 환경변수로 덮을 수 있게 두는 이유는 **테스트다** — 가짜 prior 로 각 오류 화면을
/// 재현하는 유일한 문이다. 기본은 PATH 의 prior 다.
pub fn prior_bin() -> String {
    if let Ok(p) = std::env::var("PRIORCASE_APP_BIN") {
        if !p.is_empty() {
            return p;
        }
    }
    "prior".to_string()
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
pub fn queue() -> Result<String, CmdError> {
    run_queue(&prior_bin())
}

/// settings 는 볼트·도메인·호스트를 한 번에 준다.
#[tauri::command]
pub fn settings() -> Result<String, CmdError> {
    run_settings(&prior_bin())
}

/// set_host 는 호스트 하나를 켜거나 끈다.
///
/// **앱이 설정 파일을 직접 쓰지 않는다.** 주석 보존·스칼라 볼트 변환·고친 뒤
/// 검증이 두 벌이 되면 한쪽만 고쳐진 채로 남고, 그때 망가지는 것은 사람이 손으로
/// 쓴 설정이다.
#[tauri::command]
pub fn set_host(name: String, enabled: bool) -> Result<(), CmdError> {
    let sub = if enabled { "enable" } else { "disable" };
    run(&prior_bin(), &["hosts", sub, &name], WRITE_TIMEOUT)
        .map(|_| ())
        .map_err(to_cmd_error)
}

/// add_vault 는 볼트를 하나 만든다. CLI 가 자리(폴더)까지 만든다.
#[tauri::command]
pub fn add_vault(name: String, path: String) -> Result<(), CmdError> {
    run(&prior_bin(), &["vault", "add", &name, &path], WRITE_TIMEOUT)
        .map(|_| ())
        .map_err(to_cmd_error)
}

/// bind_domain 은 프로젝트가 쓸 볼트를 정한다. vault 가 비면 기본 볼트로 되돌린다.
#[tauri::command]
pub fn bind_domain(prefix: String, vault: String) -> Result<(), CmdError> {
    let mut args = vec!["domain", "bind", prefix.as_str()];
    if !vault.is_empty() {
        args.push(vault.as_str());
    }
    run(&prior_bin(), &args, WRITE_TIMEOUT)
        .map(|_| ())
        .map_err(to_cmd_error)
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
pub fn open_vault(name: String) -> Result<(), CmdError> {
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

        let out = crate::prior::run(
            &echo,
            &["vault", "add", "회사", "/경로 with space/볼트"],
            READ_TIMEOUT,
        )
        .expect("성공해야 한다");
        assert!(out.contains("/경로 with space/볼트"), "인자가 쪼개졌다: {out}");
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
