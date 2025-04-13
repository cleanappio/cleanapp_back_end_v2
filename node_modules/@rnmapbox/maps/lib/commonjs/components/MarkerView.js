"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
var _RNMBXMarkerViewContentNativeComponent = _interopRequireDefault(require("../specs/RNMBXMarkerViewContentNativeComponent"));
var _RNMBXMarkerViewNativeComponent = _interopRequireDefault(require("../specs/RNMBXMarkerViewNativeComponent"));
var _utils = require("../utils");
var _geoUtils = require("../utils/geoUtils");
var _PointAnnotation = _interopRequireDefault(require("./PointAnnotation"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const Mapbox = _reactNative.NativeModules.RNMBXModule;
/**
 * MarkerView represents an interactive React Native marker on the map.
 *
 * If you have static views, consider using PointAnnotation or SymbolLayer to display
 * an image, as they'll offer much better performance. Mapbox suggests using this
 * component for a maximum of around 100 views displayed at one time.
 *
 * This is implemented with view annotations on [Android](https://docs.mapbox.com/android/maps/guides/annotations/view-annotations/)
 * and [iOS](https://docs.mapbox.com/ios/maps/guides/annotations/view-annotations).
 *
 * This component has no dedicated `onPress` method. Instead, you should handle gestures
 * with the React views passed in as `children`.
 */
class MarkerView extends _react.default.PureComponent {
  static defaultProps = {
    anchor: {
      x: 0.5,
      y: 0.5
    },
    allowOverlap: false,
    allowOverlapWithPuck: false,
    isSelected: false
  };
  static lastId = 0;
  _idForPointAnnotation() {
    if (this.__idForPointAnnotation === undefined) {
      MarkerView.lastId = MarkerView.lastId + 1;
      this.__idForPointAnnotation = `MV-${MarkerView.lastId}`;
    }
    return this.__idForPointAnnotation;
  }
  _getCoordinate(coordinate) {
    if (!coordinate) {
      return undefined;
    }
    return (0, _utils.toJSONString)((0, _geoUtils.makePoint)(coordinate));
  }
  render() {
    if (this.props.anchor.x < 0 || this.props.anchor.y < 0 || this.props.anchor.x > 1 || this.props.anchor.y > 1) {
      console.warn(`[MarkerView] Anchor with value (${this.props.anchor.x}, ${this.props.anchor.y}) should not be outside the range [(0, 0), (1, 1)]`);
    }
    if (_reactNative.Platform.OS === 'ios' && !Mapbox.MapboxV10) {
      return /*#__PURE__*/(0, _jsxRuntime.jsx)(_PointAnnotation.default, {
        id: this._idForPointAnnotation(),
        ...this.props
      });
    }
    const {
      anchor = {
        x: 0.5,
        y: 0.5
      }
    } = this.props;
    return /*#__PURE__*/(0, _jsxRuntime.jsx)(RNMBXMarkerView, {
      style: [{
        flex: 0,
        alignSelf: 'flex-start'
      }, this.props.style],
      coordinate: [Number(this.props.coordinate[0]), Number(this.props.coordinate[1])],
      anchor: anchor,
      allowOverlap: this.props.allowOverlap,
      allowOverlapWithPuck: this.props.allowOverlapWithPuck,
      isSelected: this.props.isSelected,
      onTouchEnd: e => {
        e.stopPropagation();
      },
      children: /*#__PURE__*/(0, _jsxRuntime.jsx)(_RNMBXMarkerViewContentNativeComponent.default, {
        style: {
          flex: 0,
          alignSelf: 'flex-start'
        },
        onStartShouldSetResponder: _event => {
          return true;
        },
        onTouchEnd: e => {
          e.stopPropagation();
        },
        children: this.props.children
      })
    });
  }
}
const RNMBXMarkerView = _RNMBXMarkerViewNativeComponent.default;
var _default = exports.default = MarkerView;
//# sourceMappingURL=MarkerView.js.map