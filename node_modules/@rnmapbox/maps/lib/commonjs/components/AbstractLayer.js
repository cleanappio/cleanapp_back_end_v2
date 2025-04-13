"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
var _filterUtils = require("../utils/filterUtils");
var _StyleValue = require("../utils/StyleValue");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
class AbstractLayer extends _react.default.PureComponent {
  get baseProps() {
    return {
      ...this.props,
      id: this.props.id,
      existing: this.props.existing,
      sourceID: this.props.sourceID,
      reactStyle: this.getStyle(this.props.style),
      minZoomLevel: this.props.minZoomLevel,
      maxZoomLevel: this.props.maxZoomLevel,
      aboveLayerID: this.props.aboveLayerID,
      belowLayerID: this.props.belowLayerID,
      layerIndex: this.props.layerIndex,
      filter: (0, _filterUtils.getFilter)(this.props.filter),
      style: undefined
    };
  }
  nativeLayer = null;
  setNativeLayer = instance => {
    this.nativeLayer = instance;
  };
  getStyleTypeFormatter(styleType) {
    if (styleType === 'color') {
      return _reactNative.processColor;
    }
    return undefined;
  }
  getStyle(style) {
    return (0, _StyleValue.transformStyle)(style);
  }
  setNativeProps(props) {
    if (this.nativeLayer) {
      let propsToPass = props;
      if (props.style) {
        propsToPass = {
          ...props,
          reactStyle: this.getStyle(props.style)
        };
      }
      this.nativeLayer?.setNativeProps(propsToPass);
    }
  }
}
var _default = exports.default = AbstractLayer;
//# sourceMappingURL=AbstractLayer.js.map