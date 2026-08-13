use crate::prior::{run, PriorError};
use serde::Serialize;
use std::time::Duration;

/// READ_TIMEOUT 은 `prior queue` 의 상한이다.
///
/// 실측 1.0초(결정 98건 · 볼트 하나)다. 볼트가 늘면 곱해지므로 여유를 준다.
/// 이보다 오래 걸리면 앱이 멎은 것처럼 보이므로 짧게 잡는다.
pub const READ_TIMEOUT: Duration = Duration::from_secs(10);

/// WRITE_TIMEOUT 은 쓰기 명령의 상한이다.
///
/// **`prior promote` 가 판별기를 부르고 그 예산이 90초다.** 읽기 상한을 쓰면
/// 정상 승격이 타임아웃으로 보이고, 사람은 눌렀는데 아무 일도 안 난 줄 안다.
pub const WRITE_TIMEOUT: Duration = Duration::from_secs(120);

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

#[tauri::command]
pub fn queue() -> Result<String, CmdError> {
    run_queue(&prior_bin())
}

#[tauri::command]
pub fn resolve_pending(id: String) -> Result<(), CmdError> {
    run(&prior_bin(), &["pending", "--resolve", &id], WRITE_TIMEOUT)
        .map(|_| ())
        .map_err(to_cmd_error)
}

#[tauri::command]
pub fn promote(id: String) -> Result<String, CmdError> {
    run(&prior_bin(), &["promote", &id, "--json"], WRITE_TIMEOUT).map_err(to_cmd_error)
}

#[tauri::command]
pub fn review(stem: String, outcome: String) -> Result<(), CmdError> {
    run(
        &prior_bin(),
        &["review", &stem, "--outcome", &outcome],
        WRITE_TIMEOUT,
    )
    .map(|_| ())
    .map_err(to_cmd_error)
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
        assert!(out.contains("\"confirm\""), "out={out}");
    }

    // ★★ **쓰기 상한이 읽기 상한보다 훨씬 길어야 한다.**
    //
    // `prior promote` 는 판별기를 부르고 그 예산이 90초다. 읽기 상한(10초)을 쓰면
    // **정상 승격이 타임아웃으로 보인다** — 사람은 눌렀는데 아무 일도 안 난 줄 안다.
    #[test]
    fn 쓰기_상한이_판별기_예산을_담는다() {
        assert!(
            WRITE_TIMEOUT.as_secs() >= 90,
            "쓰기 상한={:?} — prior promote 의 판별기 예산 90초를 못 담는다",
            WRITE_TIMEOUT
        );
        assert!(
            READ_TIMEOUT.as_secs() <= 15,
            "읽기 상한={:?} — 큐가 이만큼 걸리면 앱이 멎은 것처럼 보인다",
            READ_TIMEOUT
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

    // ★ 인자를 그대로 넘기는지 본다. id 에 공백·한글이 섞여도 깨지면 안 된다.
    #[test]
    fn 인자를_그대로_넘긴다() {
        let echo = fixture("fake-prior-echo-args.sh");
        let out = crate::prior::run(
            &echo,
            &["pending", "--resolve", "/경로 with space.jsonl@42"],
            READ_TIMEOUT,
        )
        .expect("성공해야 한다");
        assert!(out.contains("--resolve"), "out={out}");
        assert!(
            out.contains("/경로 with space.jsonl@42"),
            "인자가 쪼개졌다: {out}"
        );
    }
}
