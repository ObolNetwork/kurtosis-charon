from charon_matrix.network_params import build_network_params, CLS, VCS, CL_IMAGES


def test_matrix_shape():
    assert len(CLS) == 6 and len(VCS) == 6
    assert CLS[0] == "lighthouse" and VCS[-1] == "vouch"


def test_generates_combo_yaml_with_node_count():
    y = build_network_params("teku", "prysm", charon_node_count=4)
    assert "charon_node_count: 4" in y
    assert CL_IMAGES["teku"] in y
    assert "charon_vc: prysm" in y
    assert 'remote_write_url: "https://vm.monitoring.gcp.obol.tech/write"' in y


def test_nimbus_vc_gets_json_requests():
    assert "CHARON_FEATURE_SET_ENABLE: json_requests" in build_network_params("teku", "nimbus")
    assert "CHARON_FEATURE_SET_ENABLE" not in build_network_params("teku", "prysm")
