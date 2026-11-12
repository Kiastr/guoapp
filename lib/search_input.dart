import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';

import 'cinema_tokens.dart';

class SearchInput extends StatefulWidget {
  const SearchInput({
    super.key,
    required this.controller,
    required this.hint,
    required this.onSearch,
    this.onChanged,
    this.suggestions,
    this.onCancel,
    this.autofocus = false,
    this.onFocusChange,
  });
  final TextEditingController controller;
  final String hint;
  final ValueChanged<String> onSearch;
  final ValueChanged<String>? onChanged;
  final Future<List<String>> Function(String)? suggestions;
  final VoidCallback? onCancel;
  final bool autofocus;

  /// [+190] 输入框焦点变化。首页搜索框改为常驻后，「最近搜索」不再跟着
  /// 展开状态常显，只在用户真的点进输入框时出现，避免首屏被挤高。
  final ValueChanged<bool>? onFocusChange;
  @override
  State<SearchInput> createState() => _SearchInputState();
}

class _SearchInputState extends State<SearchInput> {
  final _focus = FocusNode();
  Timer? _timer;
  Completer<Iterable<String>>? _pending;
  int _generation = 0;
  bool _selected = false;
  final _cache = <String, List<String>>{};

  @override
  void initState() {
    super.initState();
    _focus.addListener(_notifyFocus);
  }

  void _notifyFocus() => widget.onFocusChange?.call(_focus.hasFocus);

  void _cancel() {
    _timer?.cancel();
    widget.onCancel?.call();
    _generation++;
    if (_pending != null && !_pending!.isCompleted) {
      _pending!.complete(const []);
    }
  }

  @override
  void didUpdateWidget(SearchInput oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.suggestions == null) _cancel();
  }

  @override
  void dispose() {
    _cancel();
    _focus.dispose();
    super.dispose();
  }

  Future<Iterable<String>> _options(TextEditingValue value) async {
    _cancel();
    final query = value.text.trim();
    if (query.isEmpty ||
        query.length > 100 ||
        widget.suggestions == null ||
        !value.composing.isCollapsed) {
      return const [];
    }
    if (_cache.containsKey(query)) return _cache[query]!;
    final generation = _generation;
    final pending = Completer<Iterable<String>>();
    _pending = pending;
    _timer = Timer(const Duration(milliseconds: 300), () async {
      try {
        final result = (await widget.suggestions!(
          query,
        )).where((s) => s.trim().isNotEmpty).toSet().take(10).toList();
        if (generation == _generation && mounted) {
          if (_cache.length >= 32) _cache.remove(_cache.keys.first);
          _cache[query] = result;
          if (!pending.isCompleted) pending.complete(result);
        }
      } catch (_) {}
      if (!pending.isCompleted) pending.complete(const []);
    });
    return pending.future;
  }

  void _submit(String value) {
    _cancel();
    widget.controller.value = TextEditingValue(
      text: value,
      selection: TextSelection.collapsed(offset: value.length),
    );
    _focus.unfocus();
    widget.onSearch(value.trim());
  }

  Widget _highlight(String value) {
    final query = widget.controller.text.trim().toLowerCase();
    final spans = <TextSpan>[];
    var start = 0;
    while (query.isNotEmpty) {
      final match = value.toLowerCase().indexOf(query, start);
      if (match < 0) break;
      if (match > start) {
        spans.add(TextSpan(text: value.substring(start, match)));
      }
      spans.add(
        TextSpan(
          text: value.substring(match, match + query.length),
          style: TextStyle(
            color: Theme.of(context).colorScheme.primary,
            fontWeight: FontWeight.bold,
          ),
        ),
      );
      start = match + query.length;
    }
    if (start < value.length) spans.add(TextSpan(text: value.substring(start)));
    return Text.rich(
      TextSpan(children: spans),
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
    );
  }

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) => RawAutocomplete<String>(
      textEditingController: widget.controller,
      focusNode: _focus,
      optionsBuilder: _options,
      onSelected: (value) {
        _selected = true;
        _submit(value);
      },
      fieldViewBuilder: (context, controller, focus, submitted) {
        final scheme = Theme.of(context).colorScheme;
        final dark = Theme.of(context).brightness == Brightness.dark;
        // [+192] 搜索框对齐定稿 .search：43px 高、99px 全圆角药丸、近黑
        // 淡底 + 极弱描边、11px 提示字、17px 放大镜。空输入时右侧不放
        // 任何按钮，首屏保持定稿那样干净的胶囊右端；一旦有输入才出现
        // 清空 / 提交图标（tooltip 保持不变，search_input_test 依赖它们）。
        // 这里显式给 border，覆盖全局 inputDecorationTheme 的 16px 圆角，
        // 只影响搜索框，不动站源管理 / 密码框等其它输入框。
        final pill = OutlineInputBorder(
          borderRadius: BorderRadius.circular(99),
          borderSide: BorderSide(
            color: Colors.white.withValues(alpha: dark ? .05 : .1),
          ),
        );
        return TextField(
          controller: controller,
          focusNode: focus,
          autofocus: widget.autofocus,
          textInputAction: TextInputAction.search,
          style: TextStyle(fontSize: 12, color: scheme.onSurface),
          onChanged: (value) {
            widget.onChanged?.call(value);
            setState(() {});
          },
          onSubmitted: (value) {
            _selected = false;
            submitted();
            if (!_selected) _submit(value);
          },
          decoration: InputDecoration(
            isDense: true,
            filled: true,
            fillColor: dark
                ? CinemaTokens.surfaceDark
                : scheme.surfaceContainerHighest,
            constraints: const BoxConstraints(minHeight: 43, maxHeight: 43),
            contentPadding: const EdgeInsets.symmetric(
              horizontal: 15,
              vertical: 11,
            ),
            hintText: widget.hint,
            hintStyle: TextStyle(fontSize: 11, color: scheme.onSurfaceVariant),
            prefixIcon: const Icon(Icons.search_rounded, size: 17),
            prefixIconConstraints: const BoxConstraints(
              minWidth: 41,
              minHeight: 17,
            ),
            border: pill,
            enabledBorder: pill,
            focusedBorder: pill,
            suffixIcon: controller.text.isEmpty
                ? null
                : Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      IconButton(
                        tooltip: '清空搜索',
                        icon: const Icon(Icons.close_rounded, size: 18),
                        padding: EdgeInsets.zero,
                        constraints: const BoxConstraints(
                          minWidth: 34,
                          maxWidth: 34,
                          maxHeight: 40,
                        ),
                        onPressed: () {
                          _cancel();
                          controller.clear();
                          widget.onChanged?.call('');
                          setState(() {});
                        },
                      ),
                      IconButton(
                        tooltip: '搜索',
                        icon: const Icon(Icons.arrow_forward_rounded, size: 18),
                        padding: EdgeInsets.zero,
                        constraints: const BoxConstraints(
                          minWidth: 34,
                          maxWidth: 34,
                          maxHeight: 40,
                        ),
                        onPressed: () => _submit(controller.text),
                      ),
                      const SizedBox(width: 6),
                    ],
                  ),
          ),
        );
      },
      optionsViewBuilder: (context, selected, options) {
        final values = options.toList();
        final available =
            MediaQuery.sizeOf(context).height -
            MediaQuery.viewInsetsOf(context).bottom;
        return Align(
          alignment: Alignment.topLeft,
          child: Material(
            elevation: 8,
            borderRadius: BorderRadius.circular(CinemaTokens.panelRadius),
            clipBehavior: Clip.antiAlias,
            child: SizedBox(
              width: constraints.maxWidth,
              height: min(
                min(300.0, max(96.0, available * .45)),
                values.length * 48.0,
              ),
              child: ListView.builder(
                padding: EdgeInsets.zero,
                itemExtent: 48,
                itemCount: values.length,
                itemBuilder: (context, index) => ListTile(
                  key: ValueKey('search-suggestion-$index'),
                  selected: AutocompleteHighlightedOption.of(context) == index,
                  leading: const Icon(Icons.search, size: 18),
                  title: _highlight(values[index]),
                  onTap: () => selected(values[index]),
                ),
              ),
            ),
          ),
        );
      },
    ),
  );
}
