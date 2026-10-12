use http::{Request, Response, StatusCode, Version};

use crate::CloseReason;
use crate::client::test::scenario::write_response;

use super::scenario::Scenario;

#[test]
fn reuse_without_send_body() {
    let scenario = Scenario::builder()
        .get("https://a.test")
        .response(Response::new(()))
        .recv_body("hello", false)
        .build();

    let call = scenario.to_cleanup();

    assert!(!call.must_close_connection());
}

#[test]
fn reuse_with_send_body() {
    let scenario = Scenario::builder()
        .post("https://a.test")
        .send_body("hi", false)
        .response(Response::new(()))
        .recv_body("hello", false)
        .build();

    let call = scenario.to_cleanup();

    assert!(!call.must_close_connection());
}

#[test]
fn reuse_without_recv_body() {
    let scenario = Scenario::builder()
        .head("https://a.test")
        .response(Response::new(()))
        .build();

    let call = scenario.to_cleanup();

    assert!(!call.must_close_connection());
}

#[test]
fn reuse_after_redirect() {
    let scenario = Scenario::builder()
        .get("https://a.test")
        .redirect(StatusCode::FOUND, "https://b.test")
        .build();

    let call = scenario.to_cleanup();

    assert!(!call.must_close_connection());
}

#[test]
fn close_due_to_http10() {
    let scenario = Scenario::builder()
        .request(
            Request::get("https://a.test")
                .version(Version::HTTP_10)
                .body(())
                .unwrap(),
        )
        .build();

    let call = scenario.to_cleanup();
    let inner = call.inner();
    assert_eq!(
        *inner.close_reason.first().unwrap(),
        CloseReason::CloseDelimitedBody
    );

    assert!(call.must_close_connection());
}

#[test]
fn close_due_to_client_connection_close() {
    let scenario = Scenario::builder()
        .get("https://a.test")
        .header("connection", "close")
        .build();

    let call = scenario.to_cleanup();
    let inner = call.inner();
    assert_eq!(
        *inner.close_reason.first().unwrap(),
        CloseReason::ClientConnectionClose
    );

    assert!(call.must_close_connection());
}

#[test]
fn close_due_to_server_connection_close() {
    let scenario = Scenario::builder()
        .get("https://a.test")
        .response(
            Response::builder()
                .header("connection", "close")
                .body(())
                .unwrap(),
        )
        .build();

    let call = scenario.to_cleanup();
    let inner = call.inner();
    assert_eq!(
        *inner.close_reason.first().unwrap(),
        CloseReason::ServerConnectionClose
    );

    assert!(call.must_close_connection());
}

#[test]
fn close_due_to_not_100_continue() {
    let scenario = Scenario::builder()
        .post("https://q.test")
        .header("expect", "100-continue")
        .send_body("hi", false)
        .build();

    let mut call = scenario.to_await_100();

    let input = write_response(
        &Response::builder()
            .status(StatusCode::FORBIDDEN)
            .body(())
            .unwrap(),
    );
    call.try_read_100(&input).unwrap();

    let inner = call.inner();
    assert_eq!(
        *inner.close_reason.first().unwrap(),
        CloseReason::Not100Continue
    );
}

#[test]
fn close_due_to_close_delimited_body() {
    // no content-length or transfer-encoding
    let scenario = Scenario::builder().get("https://a.test").build();

    let call = scenario.to_cleanup();
    let inner = call.inner();
    assert_eq!(
        *inner.close_reason.first().unwrap(),
        CloseReason::CloseDelimitedBody
    );

    assert!(call.must_close_connection());
}

// Connection options are comma-separated tokens compared case-insensitively.
// https://www.rfc-editor.org/rfc/rfc9110#section-7.6.1

#[test]
fn close_due_to_server_connection_close_capitalized() {
    let scenario = Scenario::builder()
        .get("https://a.test")
        .response(
            Response::builder()
                .header("connection", "Close")
                .body(())
                .unwrap(),
        )
        .build();

    let call = scenario.to_cleanup();
    let inner = call.inner();
    assert!(
        inner
            .close_reason
            .contains(&CloseReason::ServerConnectionClose)
    );

    assert!(call.must_close_connection());
}

#[test]
fn close_due_to_server_connection_close_in_list() {
    let scenario = Scenario::builder()
        .get("https://a.test")
        .response(
            Response::builder()
                .header("connection", "keep-alive, close")
                .body(())
                .unwrap(),
        )
        .build();

    let call = scenario.to_cleanup();
    let inner = call.inner();
    assert!(
        inner
            .close_reason
            .contains(&CloseReason::ServerConnectionClose)
    );

    assert!(call.must_close_connection());
}

#[test]
fn close_due_to_client_connection_close_capitalized() {
    let scenario = Scenario::builder()
        .get("https://a.test")
        .header("connection", "Close")
        .build();

    let call = scenario.to_cleanup();
    let inner = call.inner();
    assert!(
        inner
            .close_reason
            .contains(&CloseReason::ClientConnectionClose)
    );

    assert!(call.must_close_connection());
}
