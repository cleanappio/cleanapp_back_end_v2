"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
var _Animated = _interopRequireDefault(require("../utils/animated/Animated"));
var _classes = require("../classes");
var _SymbolLayer = require("./SymbolLayer");
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
class Annotation extends _react.default.Component {
  static defaultProps = {
    animated: false,
    animationDuration: 1000,
    animationEasingFunction: _reactNative.Easing.linear
  };
  constructor(props) {
    super(props);
    const shape = this._getShapeFromProps(props);
    this.state = {
      shape: props.animated ? new _classes.AnimatedPoint(shape) : shape
    };
    this.onPress = this.onPress.bind(this);
  }
  componentDidUpdate(prevProps) {
    if (!Array.isArray(this.props.coordinates)) {
      this.setState({
        shape: null
      });
      return;
    }
    const haveCoordinatesChanged = prevProps.coordinates[0] !== this.props.coordinates[0] || prevProps.coordinates[1] !== this.props.coordinates[1];
    if (prevProps.animated !== this.props.animated || haveCoordinatesChanged && (!this.state.shape || !this.props.animated)) {
      const shape = this._getShapeFromProps(this.props);
      this.setState({
        shape: this.props.animated ? new _classes.AnimatedPoint(shape) : shape
      });
    } else if (haveCoordinatesChanged && this.props.animated && this.state.shape) {
      // flush current animations
      this.state.shape.stopAnimation();
      this.state.shape.timing({
        coordinates: this.props.coordinates,
        easing: this.props.animationEasingFunction,
        duration: this.props.animationDuration
      }).start();
    }
  }
  onPress(event) {
    if (this.props.onPress) {
      this.props.onPress(event);
    }
  }
  _getShapeFromProps(props = {}) {
    const lng = props.coordinates?.[0] || 0;
    const lat = props.coordinates?.[1] || 0;
    return {
      type: 'Point',
      coordinates: [lng, lat]
    };
  }
  get symbolStyle() {
    if (!this.props.icon) {
      return undefined;
    }
    return Object.assign({}, this.props.style, {
      iconImage: this.props.icon
    });
  }
  render() {
    if (!this.props.coordinates) {
      return null;
    }
    const children = [];
    if (this.symbolStyle) {
      children.push(/*#__PURE__*/(0, _jsxRuntime.jsx)(_SymbolLayer.SymbolLayer, {
        id: `${this.props.id}-symbol`,
        style: this.symbolStyle
      }));
    }
    if (this.props.children) {
      if (Array.isArray(this.props.children)) {
        children.push(...this.props.children);
      } else {
        children.push(this.props.children);
      }
    }
    return /*#__PURE__*/(0, _jsxRuntime.jsx)(_Animated.default.ShapeSource, {
      id: this.props.id,
      onPress: this.onPress,
      shape: this.state.shape,
      children: children
    });
  }
}
var _default = exports.default = Annotation;
//# sourceMappingURL=Annotation.js.map