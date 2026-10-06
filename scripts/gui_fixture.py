#!/usr/bin/env python3
"""真实 GUI 验证专用窗口；需可选验证依赖 PySide6，不属于服务运行依赖。"""
import argparse
import json
from pathlib import Path
import sys

from PySide6.QtCore import QMimeData, QRect, Qt, QTimer
from PySide6.QtGui import QColor, QGuiApplication, QKeySequence, QPainter
from PySide6.QtWidgets import QApplication, QLineEdit, QMessageBox, QPushButton, QWidget

from gui_clipboard import KDE_CONTROL_MIME, REFRESH_ERRORS, RESCUE_ERRORS, copy_snapshot, republish_same, rescue_snapshot, summarize, validate_original_formats


MARKERS = [(20, 20, "#f10d4e"), (600, 20, "#17d6b9"), (20, 420, "#d94a07"), (600, 420, "#493cf1")]


class VerificationField(QLineEdit):
    def keyPressEvent(self, event):
        # 完整文字验收需可见中文；直接全选避免普通Qt快捷键尾部自动复制PRIMARY。
        if event.matches(QKeySequence.StandardKey.SelectAll):
            self.selectAll()
            event.accept()
            return
        super().keyPressEvent(event)


class Target(QWidget):
    def __init__(self, evidence, inputs_only=False):
        super().__init__()
        self.evidence = evidence
        self.events = []
        self.drag_origin = None
        self.clipboard_revision = 0
        self.original_snapshot = None
        self.original_baseline = None
        self.recovery_pending = False
        self.offered_data = None
        self.publishing_offer = False
        self.clipboard = QApplication.clipboard()
        self.clipboard.dataChanged.connect(self.clipboard_changed)
        self.setWindowTitle("GUI 验证窗口")
        self.setFixedSize(640, 460)
        self.field = VerificationField(self)
        self.field.setGeometry(50, 100, 540, 60)
        self.field.setStyleSheet("background: white; color: black; font-size: 22px;")
        self.field.textChanged.connect(lambda _: self.save())
        self.restore_button = QPushButton("恢复测试前剪贴板", self)
        self.restore_button.setGeometry(200, 390, 240, 40)
        self.restore_button.clicked.connect(self.restore_by_user)
        self.restore_button.hide()
        self.requests = self.evidence.with_name("clipboard-request.json")
        self.responses = self.evidence.with_name("clipboard-response.json")
        self.last_clipboard_request = None
        self.last_focus_request = None
        self.timer = QTimer(self)
        self.timer.timeout.connect(self.sample_clipboard)
        self.timer.start(50)
        if inputs_only:
            # 普通全选会由Qt自动发布PRIMARY选区；密码回显禁用复制，仍验证真实全选与删除。
            self.field.setEchoMode(QLineEdit.EchoMode.Password)
            self.field.setText("组合键验证")
        self.save()

    def clipboard_changed(self):
        self.clipboard_revision += 1
        self.release_offered_data()

    def release_offered_data(self):
        # C++所有权已转移；仅明确失去所有权且独立备份存在时放下Python强引用。
        if not self.publishing_offer and not self.clipboard.ownsClipboard() and self.original_snapshot is not None:
            self.offered_data = None

    def read_clipboard_snapshot(self):
        """同内容发布只读准备；失焦、格式或版本变化时不发布。"""
        if not self.isActiveWindow():
            raise RuntimeError("窗口未获得读取焦点")
        revision = self.clipboard_revision
        contents = self.clipboard.mimeData()
        formats = contents.formats() if contents is not None else []
        summary, data = copy_snapshot(formats, contents.data if contents is not None else lambda _: b"")
        current = self.clipboard.mimeData()
        if not self.isActiveWindow() or revision != self.clipboard_revision or formats != (current.formats() if current is not None else []):
            raise RuntimeError("剪贴板在读取期间发生变化")
        return summary, data, revision

    def publish_clipboard_snapshot(self, contents):
        # Qt 接管提供者对象；后续新所有者或窗口退出时释放，不把字节写盘。
        if KDE_CONTROL_MIME in contents:
            raise RuntimeError("rescue_control_format_unsupported")
        data = QMimeData()
        for mime, value in contents.items():
            data.setData(mime, value)
        self.offered_data = data
        self.publishing_offer = True
        try:
            self.clipboard.setMimeData(data)
        finally:
            self.publishing_offer = False

    def publish_original_offer(self, contents):
        # 第一次显式发布前保留有界原数据；Qt失去所有权也不能销毁唯一备份。
        if self.original_snapshot is None:
            self.original_snapshot = dict(contents)
            self.recovery_pending = True
            self.restore_button.show()
        self.publish_clipboard_snapshot(contents)

    def restore_by_user(self):
        """按钮点击是用户明确恢复请求，原数据不展示或写入证据文件。"""
        if self.original_snapshot is None:
            return
        try:
            payload = self.original_snapshot
            baseline = self.original_baseline
            omitted_control = KDE_CONTROL_MIME in payload
            if omitted_control:
                answer = QMessageBox.question(self, "剪贴板恢复", "原备份含 KDE 所有权控制标记，原样发布会被拒绝。是否仅恢复有效数据格式，并明确省略这个控制标记？",
                                              QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.No, QMessageBox.StandardButton.No)
                if answer != QMessageBox.StandardButton.Yes:
                    return
                # 只有用户明确同意才省略控制标记；原备份保持完整，自动准备/救援绝不剥除。
                payload = {mime: value for mime, value in payload.items() if mime != KDE_CONTROL_MIME}
                baseline = summarize(list(payload), payload.__getitem__)
            self.publish_clipboard_snapshot(payload)
            summary, _, _ = self.read_clipboard_snapshot()
            validate_original_formats(summary, baseline, True)
            self.recovery_pending = False
            self.restore_button.hide()
            if omitted_control:
                QMessageBox.information(self, "剪贴板恢复", "有效数据摘要已核对，控制标记未复制；原格式集合未完全恢复。")
        except Exception:
            QMessageBox.warning(self, "剪贴板恢复", "恢复未能确认，内存备份仍保留。")

    def closeEvent(self, event):
        if self.recovery_pending and self.original_snapshot is not None:
            answer = QMessageBox.question(self, "保留剪贴板备份", "关闭窗口会丢失尚未恢复的内存备份。是否仍关闭？",
                                          QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.No, QMessageBox.StandardButton.No)
            if answer != QMessageBox.StandardButton.Yes:
                event.ignore()
                return
        self.original_snapshot = None
        super().closeEvent(event)

    def sample_clipboard(self):
        if not self.requests.exists():
            return
        request = json.loads(self.requests.read_text(encoding="utf-8"))
        request_id = request.get("id")
        if not isinstance(request_id, str) or request_id == self.last_clipboard_request:
            return
        response = {"id": request_id, "ok": False}
        try:
            # Wayland 对后台窗口的剪贴板读取可能返回空数据；不能把失焦当成原内容为空。
            if not self.isActiveWindow():
                if self.last_focus_request != request_id:
                    self.last_focus_request = request_id
                    self.activateWindow()
                response["error"] = "not_focused"
                return
            self.last_clipboard_request = request_id
            if request.get("operation") == "focus":
                response["ok"] = True
                return
            if request.get("operation") == "refresh_offer":
                baseline = request.get("baseline")
                if not isinstance(baseline, dict):
                    raise RuntimeError("refresh_baseline_missing")
                self.original_baseline = baseline
                summary = republish_same(self.read_clipboard_snapshot, self.publish_original_offer, baseline)
                response.update(ok=True, summary=summary, qt_derived_alias_added=validate_original_formats(summary, baseline, True))
                return
            if request.get("operation") == "rescue_original":
                if self.original_snapshot is None:
                    response.update(ok=True, recovery="backup_unavailable")
                    return
                status = rescue_snapshot(self.read_clipboard_snapshot, self.publish_clipboard_snapshot, self.original_snapshot,
                                         self.original_baseline, request.get("test_hashes", []),
                                         lambda: request.get("test_closed") is True and self.clipboard.ownsClipboard())
                self.recovery_pending = status == "needs_user"
                self.restore_button.setVisible(self.recovery_pending)
                response.update(ok=True, recovery=status, backup_retained=True, backup_provider_owned=self.clipboard.ownsClipboard())
                return
            revision = self.clipboard_revision
            contents = self.clipboard.mimeData()
            formats = contents.formats() if contents is not None else []
            # Qt 会先物化单个格式；摘要自身只保留有界哈希和长度，不复制超限内容。
            summary = summarize(formats, contents.data if contents is not None else lambda _: b"")
            current = self.clipboard.mimeData()
            if not self.isActiveWindow() or revision != self.clipboard_revision or formats != (current.formats() if current is not None else []):
                raise RuntimeError("剪贴板在采样期间发生变化")
            response.update(ok=True, summary=summary)
        except Exception as error:
            # 仅固定准备阶段代号可进入报告，任意 Qt/读取异常仍脱敏。
            response["error"] = str(error) if isinstance(error, RuntimeError) and str(error) in REFRESH_ERRORS | RESCUE_ERRORS else "clipboard_snapshot_unavailable"
            if request.get("operation") == "rescue_original" and self.original_snapshot is not None:
                self.recovery_pending = True
                self.restore_button.show()
        finally:
            temporary = self.responses.with_suffix(".tmp")
            temporary.write_text(json.dumps(response, ensure_ascii=False), encoding="utf-8")
            temporary.replace(self.responses)

    def save(self):
        document = {"platform": QGuiApplication.platformName(), "text": self.field.text(), "events": self.events}
        temporary = self.evidence.with_suffix(".tmp")
        temporary.write_text(json.dumps(document, ensure_ascii=False), encoding="utf-8")
        temporary.replace(self.evidence)

    def record(self, kind, position=None, **extra):
        event = {"type": kind, **extra}
        if position:
            event.update(x=position.x(), y=position.y())
        self.events.append(event)
        self.save()

    def paintEvent(self, event):
        painter = QPainter(self)
        painter.fillRect(self.rect(), QColor("#e5e8eb"))
        for x, y, color in MARKERS:
            painter.fillRect(QRect(x, y, 20, 20), QColor(color))
        painter.fillRect(QRect(50, 200, 540, 180), QColor("#5a7087"))

    def mousePressEvent(self, event):
        if event.button() == Qt.MouseButton.LeftButton:
            self.drag_origin = event.position()
        self.record("press", event.position())

    def mouseReleaseEvent(self, event):
        position = event.position()
        if self.drag_origin is not None and (position - self.drag_origin).manhattanLength() > 30:
            self.record("drag", position)
        self.drag_origin = None
        self.record("release", position)

    def mouseDoubleClickEvent(self, event):
        self.record("double_click", event.position())

    def wheelEvent(self, event):
        self.record("scroll", event.position(), delta_x=event.angleDelta().x(), delta_y=event.angleDelta().y())

    def keyPressEvent(self, event):
        self.record("key", key=event.key())
        super().keyPressEvent(event)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--evidence", required=True)
    parser.add_argument("--inputs-only", action="store_true", help="仅用固定字段验证键鼠，不准备剪贴板")
    args = parser.parse_args()
    application = QApplication(sys.argv[:1])
    target = Target(Path(args.evidence), inputs_only=args.inputs_only)
    target.show()
    target.activateWindow()
    return application.exec()


if __name__ == "__main__":
    sys.exit(main())
