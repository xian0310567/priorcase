// Windows 릴리스에서 콘솔 창이 뜨지 않게 한다. macOS 에서는 무해하다.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use tauri::{
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    Manager,
};

fn main() {
    tauri::Builder::default()
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
                        let app = tray.app_handle();
                        if let Some(w) = app.get_webview_window("main") {
                            if w.is_visible().unwrap_or(false) {
                                let _ = w.hide();
                            } else {
                                let _ = w.show();
                                let _ = w.set_focus();
                            }
                        }
                    }
                })
                .build(app)?;
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("앱을 실행할 수 없다");
}
