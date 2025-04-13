"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = exports.AnimatedShape = void 0;
var _reactNative = require("react-native");
// see
// https://github.com/facebook/react-native/blob/master/Libraries/Animated/src/nodes/AnimatedWithChildren.js
const AnimatedWithChildren = Object.getPrototypeOf(_reactNative.Animated.ValueXY);
if (__DEV__) {
  if (AnimatedWithChildren.name !== 'AnimatedWithChildren') {
    console.error('AnimatedShape could not obtain AnimatedWithChildren base class');
  }
}

/**
 * AnimatedShape can be used to display an animated LineString, FeatureCollection, Point, or other struture inside the shape property.
 * @example
 * const animatedLon = useRef(new Animated.Value(-73.984638)).current;
 * const animatedLat = useRef(new Animated.Value(40.759211)).current;
 * const animatedShape = new AnimatedShape({
 *   type: 'LineString',
 *   coordinates: [animatedLon, animatedLat],
 * });
 * return <AnimatedShapeSource shape={animatedShape} />
 */
class AnimatedShape extends AnimatedWithChildren {
  // equivalent of AnimatedStyle for shapes
  // https://github.com/facebook/react-native/blob/master/Libraries/Animated/src/nodes/AnimatedStyle.js

  constructor(shape) {
    super();
    this.shape = shape;
  }
  _walkShapeAndGetValues(value) {
    if (Array.isArray(value)) {
      return value.map(i => this._walkShapeAndGetValues(i));
    }
    if (value instanceof _reactNative.Animated.Node) {
      return value.__getValue();
    }
    if (typeof value === 'object') {
      const result = {};
      for (const key in value) {
        result[key] = this._walkShapeAndGetValues(value[key]);
      }
      return result;
    }
    return value;
  }
  __getValue() {
    const result = this._walkShapeAndGetValues(this.shape);
    return result;
  }
  _walkAndProcess(value, cb) {
    if (Array.isArray(value)) {
      value.forEach(i => this._walkAndProcess(i, cb));
    } else if (value instanceof _reactNative.Animated.Node) {
      cb(value);
    } else if (typeof value === 'object') {
      for (const key in value) {
        this._walkAndProcess(value[key], cb);
      }
    }
  }
  __attach() {
    this._walkAndProcess(this.shape, v => v.__addChild(this));
  }
  __detach() {
    this._walkAndProcess(this.shape, v => v.__removeChild(this));
    super.__detach();
  }
}
exports.AnimatedShape = AnimatedShape;
var _default = exports.default = AnimatedShape;
//# sourceMappingURL=AnimatedShape.js.map