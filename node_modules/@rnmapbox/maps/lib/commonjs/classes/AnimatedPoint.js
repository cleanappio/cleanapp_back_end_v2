"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = exports.AnimatedPoint = void 0;
var _reactNative = require("react-native");
// Used react-native-maps as a reference
// https://github.com/react-community/react-native-maps/blob/master/lib/components/AnimatedRegion.js
const AnimatedWithChildren = Object.getPrototypeOf(_reactNative.Animated.ValueXY);
const DEFAULT_COORD = [0, 0];
const DEFAULT_POINT = {
  type: 'Point',
  coordinates: DEFAULT_COORD
};
let uniqueID = 0;
class AnimatedPoint extends AnimatedWithChildren {
  constructor(point = DEFAULT_POINT) {
    super();
    this.longitude = point.coordinates[0] || 0;
    this.latitude = point.coordinates[1] || 0;
    if (!(this.longitude instanceof _reactNative.Animated.Value)) {
      this.longitude = new _reactNative.Animated.Value(this.longitude);
    }
    if (!(this.latitude instanceof _reactNative.Animated.Value)) {
      this.latitude = new _reactNative.Animated.Value(this.latitude);
    }
    this._listeners = {};
  }
  setValue(point = DEFAULT_POINT) {
    this.longitude.setValue(point.coordinates[0]);
    this.latitude.setValue(point.coordinates[1]);
  }
  setOffset(point = DEFAULT_POINT) {
    this.longitude.setOffset(point.coordinates[0]);
    this.latitude.setOffset(point.coordinates[1]);
  }
  flattenOffset() {
    this.longitude.flattenOffset();
    this.latitude.flattenOffset();
  }
  stopAnimation(cb) {
    this.longitude.stopAnimation();
    this.latitude.stopAnimation();
    if (typeof cb === 'function') {
      cb(this.__getValue());
    }
  }
  addListener(cb) {
    uniqueID += 1;
    const id = `${String(uniqueID)}-${String(Date.now())}`;
    const completeCB = () => {
      if (typeof cb === 'function') {
        cb(this.__getValue());
      }
    };
    this._listeners[id] = {
      longitude: this.longitude.addListener(completeCB),
      latitude: this.latitude.addListener(completeCB)
    };
    return id;
  }
  removeListener(id) {
    this.longitude.removeListener(this._listeners[id].longitude);
    this.latitude.removeListener(this._listeners[id].latitude);
    delete this._listeners[id];
  }
  spring(config = {
    coordinates: DEFAULT_COORD
  }) {
    return _reactNative.Animated.parallel([_reactNative.Animated.spring(this.longitude, {
      ...config,
      toValue: config.coordinates[0],
      useNativeDriver: false
    }), _reactNative.Animated.spring(this.latitude, {
      ...config,
      toValue: config.coordinates[1],
      useNativeDriver: false
    })]);
  }
  timing(config = {
    coordinates: DEFAULT_COORD
  }) {
    return _reactNative.Animated.parallel([_reactNative.Animated.timing(this.longitude, {
      ...config,
      toValue: config.coordinates[0],
      useNativeDriver: false
    }), _reactNative.Animated.timing(this.latitude, {
      ...config,
      toValue: config.coordinates[1],
      useNativeDriver: false
    })]);
  }
  __getValue() {
    return {
      type: 'Point',
      coordinates: [this.longitude.__getValue(), this.latitude.__getValue()]
    };
  }
  __attach() {
    this.longitude.__addChild(this);
    this.latitude.__addChild(this);
  }
  __detach() {
    this.longitude.__removeChild(this);
    this.latitude.__removeChild(this);
  }
}
exports.AnimatedPoint = AnimatedPoint;
var _default = exports.default = AnimatedPoint;
//# sourceMappingURL=AnimatedPoint.js.map