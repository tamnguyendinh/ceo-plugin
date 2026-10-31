# Lịch sử Thay đổi (Change Log) — CEO Plugin

Tài liệu ghi nhận toàn bộ quá trình cải tiến và phát triển của **CEO Plugin** qua từng phiên bản.

---

## [v2.0.0] - 2026-10-26: Chuẩn hóa Danh xưng Thương hiệu `CEO Plugin`
- **Đổi tên toàn diện:** Đổi tên chính thức từ `anvien-ceo-plugin` sang **`ceo-plugin`** (Thương hiệu hiển thị: **`CEO Plugin`**).
- **Phân định ranh giới rạch ròi:** Xóa bỏ hoàn toàn sự nhầm lẫn giữa Plugin điều phối tác nghiệp (`ceo-plugin`) và Tool phân tích đồ thị mã nguồn (`Anvien`).
- **Chuẩn hóa hệ sinh thái:** Đồng bộ toàn diện tên lệnh CLI (`ceo-plugin <lane>`), 3 file nhị phân (`ceo-plugin*.exe`), Go module (`module ceo-plugin`), thư mục cài đặt (`~/.ceo-plugin/`), Windows Registry, cấu hình MCP Server, và thẻ quản lý ngữ cảnh AI (`<!-- ceo-plugin:start -->`).
- **Tự động dọn rác bản cũ:** Bộ cài đặt `ceo-plugin-setup.exe` tự động phát hiện và xóa sạch cấu hình MCP cũ của `anvien-ceo-plugin` trong các file cài đặt IDE.

---

## [v1.5.0] - 2026-10-26: Nạp Kỹ năng Trực tiếp qua Bộ nhớ (In-Memory Runtime Streaming)
- **Nạp trọn gói thư mục kỹ năng:** Khi chạy lệnh của bất kỳ vai trò nào (`ceo-plugin qa`, `ceo-plugin supervisor`...), công cụ nạp trọn vẹn cả kỹ năng cốt lõi (`SKILL.md`) cùng toàn bộ các tài liệu hướng dẫn chuyên sâu (`references/`) hoặc biểu mẫu (`templates/`) qua luồng dữ liệu màn hình (Stdout Pipe).
- **Bảo mật bí mật công nghệ:** Dữ liệu kỹ năng được nạp thẳng vào bộ nhớ RAM của AI, tuyệt đối không tạo thêm file rác trên ổ cứng máy tính.
- **Khóa quyền tự chủ cho AI cấp dưới:** Quy định rõ ràng trong hợp đồng: Subagent Lane tự chạy lệnh nạp kỹ năng của chính mình, giảm tải tối đa bộ nhớ ngữ cảnh cho CEO.
- **Dọn sạch kho lưu trữ Git:** Quy tụ toàn bộ file nhị phân vào thư mục `dist/bin/` và đưa vào `.gitignore`, giữ kho mã nguồn dự án luôn sạch sẽ.

---

## [v1.4.0] - 2026-10-26: Chuẩn hóa Quốc tế Bảng Hướng dẫn Lệnh
- **Bảng điều hướng 2 cột chuẩn quốc tế:** Tối ưu hóa bảng "Command Selection Guide" sang 100% tiếng Anh súc tích (`When you need to... | Use command`) để các mô hình ngôn ngữ lớn (LLM) phân tích cú pháp nhanh hơn và tiết kiệm token.
- **Che giấu chi tiết nội bộ:** Loại bỏ cột mô tả chi tiết file cài đặt để giữ giao diện bảng lệnh gọn gàng và bảo mật.

---

## [v1.3.0] - 2026-10-26: Tự động Đồng bộ Ngữ cảnh AI
- **Tự động cập nhật `AGENTS.md` & `CLAUDE.md`:** Mỗi khi chạy bất kỳ câu lệnh nào của công cụ, bảng hướng dẫn chọn vai trò sẽ tự động được tiêm hoặc cập nhật mới nhất vào các file cấu hình AI gốc của kho mã nguồn.
- **Tách bạch quy chế giám sát:** Tách riêng quy chế chờ lệnh (Standby) và bộ đếm nhịp định kỳ (Liveness Timer Heartbeat) cho CEO điều phối.

---

## [v1.2.0] - 2026-10-26: Bề mặt Lệnh CLI Chuyên môn & Tự nhận diện Môi trường
- **Hỗ trợ đầy đủ 8 vai trò chuyên môn:** Bổ sung các lệnh dòng lệnh trực tiếp cho: `ceo`, `system-architect`, `architect`, `coder`, `qa`, `supervisor`, `mechanical-planner`, `strategic-planner`.
- **Tự động nhận diện IDE:** Tự động phát hiện môi trường soạn thảo đang chạy (Google Antigravity, Codex App, Cursor) để đưa ra chỉ dẫn khởi tạo tác nhân phù hợp nhất.

---

## [v1.1.0] - 2026-10-26: Bộ 3 Nhị phân Độc lập & Đăng ký Windows Apps
- **Tách biệt 3 chức năng nhị phân:**
  1. Lõi Engine CLI & MCP Server.
  2. Trình cài đặt 1-Click All-in-One (`setup.exe`).
  3. Trình gỡ cài đặt chuyên biệt (`uninstall.exe`).
- **Tự động thêm vào PATH:** Người dùng và AI có thể gọi lệnh từ bất kỳ thư mục hay ổ đĩa nào trong máy tính.
- **Tích hợp sâu Windows Apps:** Đăng ký trực tiếp vào hệ thống quản lý ứng dụng của Windows (Settings > Apps > Installed apps) để người dùng có thể gỡ bỏ nhanh chóng.

---

## [v1.0.0] - 2026-10-25: Bản Khởi tạo Lõi Điều phối
- **Đóng gói nhị phân độc lập:** Biên dịch mã máy nhị phân chạy trực tiếp trên Windows.
- **Tích hợp MCP Server Stdio:** Hỗ trợ kết nối chuẩn Model Context Protocol (MCP) qua luồng Stdio cho các trợ lý lập trình AI hàng đầu.
