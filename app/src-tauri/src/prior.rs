use std::io::Read;
use std::process::{Command, Stdio};
use std::sync::mpsc;
use std::time::Duration;

/// PriorError 는 앱이 **서로 다르게 그려야 하는** 실패들이다.
///
/// 한 종류로 뭉치면 "설치가 안 됐다" 와 "설정이 깨졌다" 와 "느리다" 가 같은
/// 화면이 되고, 사람은 무엇을 고쳐야 할지 모른다.
#[derive(Debug)]
pub enum PriorError {
    /// 바이너리 자체가 없다 → 설치 안내 화면
    NotFound,
    /// 돌았지만 0 아닌 코드로 끝났다 → stderr 를 그대로 보여 준다
    Failed { code: Option<i32>, stderr: String },
    /// 상한을 넘겼다 → "느리다" 는 별개 사실이다
    Timeout,
    /// 그 밖 (파이프 실패 등)
    Io(String),
}

impl std::fmt::Display for PriorError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            PriorError::NotFound => write!(f, "prior 를 찾을 수 없다"),
            PriorError::Failed { code, stderr } => {
                write!(f, "prior 가 실패했다 (코드 {code:?}): {stderr}")
            }
            PriorError::Timeout => write!(f, "prior 가 상한 안에 끝나지 않았다"),
            PriorError::Io(m) => write!(f, "prior 를 실행할 수 없다: {m}"),
        }
    }
}

/// run 은 prior 를 한 번 부르고 stdout 을 준다.
///
/// **cwd 를 $HOME 으로 고정한다.** 앱 번들 안에서 돌면 `.app` 이 읽기 전용일 때
/// 이상하게 실패하고, 무엇보다 prior 의 일부 명령이 cwd 로 볼트를 고른다 —
/// 앱이 부르는 명령은 그렇지 않지만, 우연히 그런 명령을 추가했을 때 앱 번들
/// 경로가 도메인 해석에 들어가는 일을 막는다.
pub fn run(bin: &str, args: &[&str], timeout: Duration) -> Result<String, PriorError> {
    let home = std::env::var("HOME").unwrap_or_else(|_| "/".to_string());
    let mut child = match Command::new(bin)
        .args(args)
        .current_dir(home)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
    {
        Ok(c) => c,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Err(PriorError::NotFound),
        Err(e) => return Err(PriorError::Io(e.to_string())),
    };

    // **파이프를 먼저 스레드로 빨아들인다.**
    //
    // 안 그러면 출력이 파이프 버퍼(보통 64KB)를 넘는 순간 자식이 write 에서
    // 블록되고, 우리는 상한까지 기다린 뒤 Timeout 을 낸다 — 그건 느린 명령이
    // 아니라 **우리가 만든 교착**이다. prior queue 의 출력은 발췌와 볼트 대조
    // 때문에 실제로 수백 KB 가 된다.
    let mut so = child.stdout.take().expect("stdout 파이프");
    let mut se = child.stderr.take().expect("stderr 파이프");
    let (tx_o, rx_o) = mpsc::channel();
    std::thread::spawn(move || {
        let mut s = String::new();
        let _ = so.read_to_string(&mut s);
        let _ = tx_o.send(s);
    });
    let (tx_e, rx_e) = mpsc::channel();
    std::thread::spawn(move || {
        let mut s = String::new();
        let _ = se.read_to_string(&mut s);
        let _ = tx_e.send(s);
    });

    let deadline = std::time::Instant::now() + timeout;
    let status = loop {
        match child.try_wait() {
            Ok(Some(st)) => break st,
            Ok(None) => {
                if std::time::Instant::now() >= deadline {
                    // **죽이고 거둔다.** kill 만 하고 wait 를 안 하면 좀비가 남는다.
                    let _ = child.kill();
                    let _ = child.wait();
                    return Err(PriorError::Timeout);
                }
                std::thread::sleep(Duration::from_millis(20));
            }
            Err(e) => return Err(PriorError::Io(e.to_string())),
        }
    };

    let stdout = rx_o.recv_timeout(Duration::from_secs(2)).unwrap_or_default();
    let stderr = rx_e.recv_timeout(Duration::from_secs(2)).unwrap_or_default();

    if status.success() {
        Ok(stdout)
    } else {
        Err(PriorError::Failed {
            code: status.code(),
            stderr,
        })
    }
}

#[cfg(test)]
// 테스트 이름을 한국어로 쓴다 — 실패했을 때 무엇이 깨졌는지 그 줄만 읽고 알 수
// 있어야 한다. rustc 의 snake_case 규약은 여기서 값이 없다.
#[allow(non_snake_case)]
mod tests {
    use super::*;
    use std::sync::Mutex;
    use std::time::Duration;

    /// SPAWN_LOCK 은 **느린 자식을 죽이는 테스트끼리** 겹치지 않게 한다.
    ///
    /// 좀비 검사는 "우리가 부모인 프로세스 중 상태가 Z 인 것" 을 센다. 그런데
    /// 테스트는 한 프로세스 안에서 병렬로 도므로, 다른 테스트가 방금 죽인 자식이
    /// 아직 거둬지기 전이면 그것까지 세어 버린다 — 실측으로 4회 중 1회 실패했다.
    ///
    /// 깜빡이는 테스트는 **신호를 잃는다.** 사람이 "또 그거네" 하고 넘기는 순간
    /// 진짜 회귀도 같이 넘어간다.
    static SPAWN_LOCK: Mutex<()> = Mutex::new(());

    fn fixture(name: &str) -> String {
        // src-tauri/ 기준으로 ../fixtures/
        let mut p = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        p.pop();
        p.push("fixtures");
        p.push(name);
        p.to_string_lossy().into_owned()
    }

    #[test]
    fn 정상_출력을_그대로_준다() {
        let out = run(
            &fixture("fake-prior-ok.sh"),
            &["queue", "--json"],
            Duration::from_secs(5),
        )
        .expect("성공해야 한다");
        assert!(out.contains("\"confirm\""), "출력이 이상하다: {out}");
    }

    #[test]
    fn 없는_바이너리는_NotFound_다() {
        let err = run("/없는/자리/prior", &["queue"], Duration::from_secs(5))
            .expect_err("실패해야 한다");
        assert!(matches!(err, PriorError::NotFound), "err={err:?}");
    }

    #[test]
    fn 종료코드가_0이_아니면_stderr_를_담는다() {
        let err = run(
            &fixture("fake-prior-exit1.sh"),
            &["queue"],
            Duration::from_secs(5),
        )
        .expect_err("실패해야 한다");
        match err {
            PriorError::Failed { code, stderr } => {
                assert_eq!(code, Some(1));
                // **stderr 를 버리면 안 된다.** 사람이 고칠 유일한 단서다.
                assert!(
                    stderr.contains("설정을 읽을 수 없다"),
                    "stderr={stderr}"
                );
            }
            other => panic!("Failed 여야 한다: {other:?}"),
        }
    }

    #[test]
    fn 상한을_넘으면_Timeout_이다() {
        let _g = SPAWN_LOCK.lock().unwrap();
        let err = run(
            &fixture("fake-prior-slow.sh"),
            &["queue"],
            Duration::from_millis(300),
        )
        .expect_err("실패해야 한다");
        assert!(matches!(err, PriorError::Timeout), "err={err:?}");
    }

    // ★★ **파이프를 안 비우면 자식이 멈춘다.**
    //
    // stdout 을 스레드로 먼저 빨아들이지 않으면, 출력이 파이프 버퍼(보통 64KB)를
    // 넘는 순간 자식이 write 에서 블록된다. 그러면 우리는 상한까지 기다린 뒤
    // Timeout 을 내는데, 그건 **느린 명령이 아니라 우리가 만든 교착**이다.
    //
    // prior queue 의 출력은 실제로 수백 KB 가 될 수 있다 (발췌 · 볼트 대조).
    #[test]
    fn 큰_출력도_교착없이_받는다() {
        let big = fixture("fake-prior-big.sh");
        let out = run(&big, &[], Duration::from_secs(10)).expect("성공해야 한다");
        // 스크립트가 1MB 를 낸다. 파이프 버퍼보다 훨씬 크다.
        assert!(
            out.len() > 900_000,
            "받은 크기 {} — 파이프를 안 비워서 잘렸거나 교착했다",
            out.len()
        );
    }

    // ★★ **상한을 넘긴 자식은 죽이고 거둬야 한다.**
    //
    // kill 만 하고 wait 를 안 하면 **좀비가 남는다.** Rust 의 Child 는 drop 될 때
    // 기다려 주지 않는다(문서에 명시). 앱은 30초마다 prior 를 부르므로, 상한을
    // 넘기는 판이 반복되면 좀비가 계속 쌓인다.
    //
    // pgrep 으로는 못 잡는다 — 좀비는 명령줄이 사라져서 안 걸린다. 부모가 우리인
    // 프로세스 중 상태가 Z 인 것을 직접 센다.
    #[test]
    fn 상한을_넘긴_자식은_좀비로_남지_않는다() {
        fn zombies() -> usize {
            let me = std::process::id().to_string();
            let out = std::process::Command::new("ps")
                .args(["-A", "-o", "ppid=,stat="])
                .output()
                .expect("ps");
            String::from_utf8_lossy(&out.stdout)
                .lines()
                .filter_map(|l| {
                    let mut it = l.split_whitespace();
                    let ppid = it.next()?;
                    let stat = it.next()?;
                    (ppid == me && stat.starts_with('Z')).then_some(())
                })
                .count()
        }

        // **정상 자식도 잠깐은 좀비로 보인다.**
        //
        // run() 은 20ms 간격으로 try_wait 하므로, 자식이 끝난 뒤 우리가 거두기
        // 전까지 최대 20ms 동안 상태가 Z 다. 다른 시험이 동시에 자식을 띄우면
        // 그것들이 기준선에 섞인다 — 실측으로 기준선이 3으로 잡혀 이 시험이
        // 깨졌다. 그때 잡힌 건 회귀가 아니라 **측정의 결함**이었다.
        //
        // 그래서 순간값이 아니라 **가라앉는지**를 본다. 새는 구현은 영영 안
        // 가라앉고, 지나가는 좀비는 수십 ms 안에 사라진다.
        fn settle(deadline: Duration) -> usize {
            let end = std::time::Instant::now() + deadline;
            loop {
                let n = zombies();
                if n == 0 || std::time::Instant::now() >= end {
                    return n;
                }
                std::thread::sleep(Duration::from_millis(10));
            }
        }

        let _g = SPAWN_LOCK.lock().unwrap();
        assert_eq!(
            settle(Duration::from_secs(3)),
            0,
            "시작 전부터 좀비가 남아 있다 — 다른 자식이 안 거둬진다"
        );

        let slow = fixture("fake-prior-slow.sh");
        for _ in 0..3 {
            let _ = run(&slow, &[], Duration::from_millis(150));
        }
        assert_eq!(
            settle(Duration::from_secs(3)),
            0,
            "상한을 넘긴 자식이 좀비로 남았다 — kill 뒤에 wait 를 안 하면 30초마다 쌓인다"
        );
    }
}
