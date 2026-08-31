// Windows 릴리스에서 콘솔 창이 뜨지 않게 한다. macOS 에서는 무해하다.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod commands;
mod prior;

use tauri::{
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    AppHandle, Manager,
};

/// toggle_window 는 창을 열거나 닫는다.
///
/// **트레이 클릭 핸들러와 개발용 방아쇠가 같은 함수를 부른다.** 둘이 각자
/// 구현하면 개발용으로 검증한 것이 실제 클릭 경로와 다른 코드가 되고, 그러면
/// 그 검증이 아무것도 보장하지 않는다.
fn toggle_window(app: &AppHandle) {
    let Some(w) = app.get_webview_window("main") else {
        eprintln!("main 창이 없다 — 트레이 클릭이 아무 일도 못 한다");
        return;
    };
    if w.is_visible().unwrap_or(false) {
        let _ = w.hide();
    } else {
        let _ = w.show();
        let _ = w.set_focus();
    }
}

/// watch_toggle_file 은 **개발 빌드에서만** 파일 하나를 지켜보다 창을 토글한다.
///
/// 트레이 클릭은 진짜 마우스 이벤트라 스크립트로 만들 수 없다 — 합성 클릭
/// (접근성 AXPress · CGEvent)은 사람의 커서와 화면을 빼앗으면서도 Tauri 의
/// 트레이 이벤트를 만들지 못했다. 그래서 **같은 함수를 부르는 다른 문**을 낸다.
///
/// 무엇이 검증되고 무엇이 안 되는지 분명히 해 둔다:
///
///   ✅ 창이 실제로 열리고 닫히는가 (toggle_window 전체)
///   ❌ macOS 가 트레이 클릭을 Tauri 에 전달하는가 (Tauri 의 몫)
///
/// 뒤엣것은 고장 나면 **시끄럽다** — 눌렀는데 아무 일도 안 난다. 이 프로젝트가
/// 경계하는 조용한 실패가 아니다.
///
/// `debug_assertions` 로 막았으므로 릴리스 빌드에는 이 코드가 없다.
#[cfg(debug_assertions)]
fn watch_toggle_file(app: AppHandle) {
    const TRIGGER: &str = "/tmp/priorcase-toggle";
    std::thread::spawn(move || loop {
        if std::path::Path::new(TRIGGER).exists() {
            let _ = std::fs::remove_file(TRIGGER);
            let a = app.clone();
            // **메인 스레드에서 해야 한다.** macOS 의 창 조작은 메인 스레드
            // 전용이고, 다른 스레드에서 부르면 조용히 아무 일도 안 나거나 죽는다.
            let _ = app.run_on_main_thread(move || toggle_window(&a));
        }
        std::thread::sleep(std::time::Duration::from_millis(150));
    });
}

fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            commands::queue,
            commands::settings,
            commands::set_host,
            commands::add_vault,
            commands::bind_domain,
            commands::set_vault_remote,
            commands::open_vault,
            commands::set_tray_title,
        ])
        .setup(|app| {
            // **메뉴바 상주 앱은 Dock 에 뜨면 안 된다.**
            //
            // tauri.conf.json 의 skipTaskbar 는 **macOS 에서 안 먹는다** (문서에
            // 명시). 실측으로 확인했다 — 그 설정만 두고 띄웠더니 Dock 에 아이콘이
            // 떴다. macOS 에서는 활성화 정책을 Accessory 로 바꿔야 한다.
            //
            // Accessory 는 Dock 아이콘과 메뉴바 메뉴를 없앤다. 창은 그대로 뜬다.
            // 실패해도 앱을 못 띄울 이유는 아니다 — Dock 에 뜰 뿐이다.
            // 다만 조용히 넘기지는 않는다.
            #[cfg(target_os = "macos")]
            if let Err(e) = app
                .handle()
                .set_activation_policy(tauri::ActivationPolicy::Accessory)
            {
                eprintln!("활성화 정책을 못 바꿨다 (Dock 에 뜬다): {e}");
            }

            let quit = MenuItem::with_id(app, "quit", "종료", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&quit])?;

            TrayIconBuilder::with_id("main")
                .icon(app.default_window_icon().unwrap().clone())
                // 템플릿 이미지로 두면 macOS 가 라이트/다크에 맞춰 색을 뒤집는다.
                .icon_as_template(true)
                // 왼쪽 클릭은 메뉴가 아니라 창을 연다 — 메뉴는 오른쪽 클릭이다.
                // **기본값이 true 이므로 반드시 꺼야 한다** (tauri 2.11 문서 확인).
                .show_menu_on_left_click(false)
                .menu(&menu)
                .on_menu_event(|app, event| {
                    if event.id() == "quit" {
                        app.exit(0);
                    }
                })
                .on_tray_icon_event(|tray, event| {
                    use tauri::tray::{MouseButton, MouseButtonState, TrayIconEvent};
                    if let TrayIconEvent::Click {
                        button: MouseButton::Left,
                        button_state: MouseButtonState::Up,
                        ..
                    } = event
                    {
                        toggle_window(tray.app_handle());
                    }
                })
                .build(app)?;

            #[cfg(debug_assertions)]
            watch_toggle_file(app.handle().clone());

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("앱을 실행할 수 없다");
}
